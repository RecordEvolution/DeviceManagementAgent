from twisted.internet import threads, reactor
from twisted.internet.defer import inlineCallbacks, returnValue
from twisted.internet.utils import getProcessOutput
from twisted.logger import Logger

from autobahn.twisted.util import sleep
from autobahn.twisted.component import Component, run

from autobahn.wamp import auth
from autobahn.wamp.types import RegisterOptions
from autobahn.wamp.exception import ApplicationError
import docker

from collections import defaultdict
import platform
# if platform.machine() != 'x86_64':
#     from gpiozero import LED
#     led = LED(22)
import six
import time
import argparse
import datetime
import os
import shutil
import inspect
import ast
import re
import random
import string
import unicodedata
import warnings

from pprint import pprint
import json
import tarfile
import uuid
import yaml
import pyufw
from wifi import Cell, Scheme
import itertools
import subprocess
import threading

log = Logger()

build_stream_dict = {}

pprint('[mgmt-agent] -------------------------------------------------')
if(os.environ['ENV'] == 'DEV'):
    pprint('[mgmt-agent] --- DEVELOPMENT MODE --------------------------')
    pprint('[mgmt-agent] -------------------------------------------------')
    device_secret = os.environ['SECRET']
    device_id = os.environ['SERIAL_NUMBER']
    s_key = os.environ['SWARM_KEY']
    d_key = os.environ['DEVICE_KEY']
    device_endpoint_url = os.environ['DEVICE_ENDPOINT_URL']
    docker_registry_url = os.environ['DOCKER_REGISTRY_URL']
    docker_main_repository = os.environ['DOCKER_MAIN_REPOSITORY']
else:
    with open('/home/pirate/config/device-config.yaml') as file:
        dconf = yaml.safe_load(file)

    device_secret = dconf['secret']  # '764f185f-486a-40c1-8d09-7963dc68eb14'
    # '412b085a-231c-92d1-1e12-1111ab12eb46'
    device_id = dconf['serial_number']
    s_key = dconf['swarm_key']
    d_key = dconf['device_key']
    device_endpoint_url = dconf['device_endpoint_url']
    docker_registry_url = dconf.get('docker_registry_url', os.environ.get('DOCKER_REGISTRY_URL', 'registry.reswarm.io/'))
    docker_main_repository = dconf.get('docker_main_repository', os.environ.get('DOCKER_MAIN_REPOSITORY', 'apps/'))

class threadsafe_iter:
    """Takes an iterator/generator and makes it thread-safe by
    serializing call to the `next` method of given iterator/generator.
    """
    def __init__(self, it):
        self.it = it
        self.lock = threading.Lock()
        self.canceled = False

    def __iter__(self):
        return self

    def __next__(self):
        if self.canceled:
            raise StopIteration
        with self.lock:
            return self.it.__next__()

    def cancel(self):
        self.canceled = True

class App:
    def __init__(self, wamp_comp):
        self.session = None  # "None" while we're disconnected from WAMP router
        self.id = device_id
        self.swarm_key = s_key
        self.device_key = d_key
        self.device_endpoint_url = device_endpoint_url

        pprint('[mgmt-agent] -------------------------------------------------')
        pprint('[mgmt-agent] --- Initializing Agent --------------------------')
        pprint('[mgmt-agent] -------------------------------------------------')
        # associate ourselves with WAMP session lifecycle
        wamp_comp.on('join', self.onJoin)
        wamp_comp.on('leave', self.onLeave)
        wamp_comp.on('disconnect', self.onDisconnect)
        wamp_comp.on('connectfailure', self.onConnectfailure)

    @inlineCallbacks
    def publishLogs(self, rpc_name, message):
        # self.session.log.info('[mgmt-agent] publishing logs for' + u're.mgmt.logs.' + rpc_name + '.' + self.id)
        return 'ok'
        message = str(message)
        try:
            yield self.session.publish(
                u're.mgmt.logs.' + rpc_name + '.' + self.id,
                {
                    'rpc_name': rpc_name,
                    'device_sn': self.id,
                    # 'source': 'wamp_management',
                    'message': str(message),
                    'tsp': str(datetime.datetime.utcnow().isoformat())
                }
            )
        except Exception as e:
            self.session.log.info('[mgmt-agent] Could not publish the logs: {}'.format(e))

    @inlineCallbacks
    def onJoin(self, session, details):
        self.session = session
        self.session.log.info('[mgmt-agent] joined session: {}, details: {}'.format(session, details))
        boot_config = self.read_boot_config() or ''
        cmdline = self.read_cmdline() or ''
        firewall = {}
        devinfo = None
        try:
            devinfo = yield self.session.call(u'reswarm.devices.update_device', {
                'swarm_key': self.swarm_key,
                'device_key': self.device_key,
                'status': 'CONNECTED',
                'boot_config_applied': True,
                'firewall_applied': True
            })
        except Exception as e:
            pprint('[mgmt-agent][error] Could not update device: {0}'.format(e))

        new_boot_config = ''
        new_cmdline = ''
        new_firewall = {}

        if devinfo:
            self.session.log.info('[mgmt-agent] device info: {}'.format(devinfo))
            self.device_name = devinfo[0]['name']
            new_boot_config = devinfo[0]['boot_config'] or ''
            new_cmdline = devinfo[0]['cmdline'] or ''
            try:
                new_firewall = devinfo[0]['firewall'] or {}
            except:
                new_firewall = {}

        if platform.machine() != 'x86_64':
            if (new_boot_config != '' and boot_config != new_boot_config):
                self.write_boot_config(new_boot_config)

            if (new_cmdline != '' and cmdline != new_cmdline):
                self.write_cmdline(new_cmdline)

            if (new_firewall != {} and firewall != new_firewall):
                pprint('[mgmt-agent] ----------- apply_firewall --------------')
                # apply_firewall(new_firewall)

            pprint('[mgmt-agent] ----------- BOOT CONFIG ---------------------')
            pprint(new_boot_config)
            pprint('[mgmt-agent] ---------------------------------------------')
            pprint(boot_config)
            pprint('[mgmt-agent] ----------- CMDLINE -------------------------')
            pprint(new_cmdline)
            pprint('[mgmt-agent] ---------------------------------------------')
            pprint(cmdline)
            pprint('[mgmt-agent] ----------- FIREWALL ------------------------')
            pprint(new_firewall)
            pprint('[mgmt-agent] ---------------------------------------------')
            pprint(firewall)
            pprint('[mgmt-agent] ----------- END CONFIGURATION FILES ---------')

        pprint('[mgmt-agent] Creating APP dir')
        os.makedirs('/home/pirate/APP/PROD', exist_ok=True)
        os.makedirs('/home/pirate/APP/DEV', exist_ok=True)
        os.makedirs('/home/pirate/SERVICE/', exist_ok=True)

        self.session.log.info('[mgmt-agent] session joined: {}'.format(details))

        @inlineCallbacks
        def sys_version():
            pprint('[mgmt-agent] --- Environment Variables {}'.format(os.environ))
            # res = yield getProcessOutput('/bin/bash', ('-c', 'docker version'), os.environ)
            res = yield 1 + 1
            pprint(res)

        pprint('[mgmt-agent] -------------------------------------------------')
        pprint('[mgmt-agent] -------------------------------------------------')
        pprint('[mgmt-agent] ---                                          ----')
        pprint('[mgmt-agent] ---         RESWARM Management Agent         ----')
        pprint('[mgmt-agent] ---         Version 1.4.0                    ----')
        pprint('[mgmt-agent] ---                                          ----')
        pprint('[mgmt-agent] -------------------------------------------------')
        self.version = "1.4.0"
        yield sys_version()
        pprint('[mgmt-agent] -------------------------------------------------')
        self._countdown = 5

        # Login to our private gcloud docker registry
        # The keyfile.json has been acquired from an IAM Service Account that was created in our gcloud project

        # _docker_key = 'default'
        # with open('/apps/keyfile.json', 'r') as f:
        #     _docker_key = f.read()

        # self.session.log.info('get docker registry key')
        # subprocess.check_output('docker login -u _json_key -p "{}" https://eu.gcr.io'.format(_docker_key), shell=True)
        # For some strange reason docker asks for an Email. This is simply answered with yes.
        # res = yield getProcessOutput('/bin/sh', ('-c', 'yes | docker login -u _json_key -p "$(cat /apps/keyfile.json)" https://eu.gcr.io'), os.environ)
        # self.session.log.info('docker registry set')

        @inlineCallbacks
        def is_running():
            pprint('[mgmt-agent] is running')
            x = yield self.client.nodes.list(filters={'role': 'manager'})
            z = []
            y = str(x).split('\n')
            pprint('[mgmt-agent] =============  Status =======================')
            for element in y[:-1]:
                try:
                    d = ast.literal_eval(element)
                    returnValue(True)
                except Exception as e:
                    self.session.log.error('is_running: {}'.format(e))
            pprint('[mgmt-agent] *********************************************')
            returnValue(False)

        def device_handshake():
            pprint('[mgmt-agent] device handshake')
            try:
                return {u'tsp': datetime.datetime.utcnow().isoformat(), u'id': self.id}
            except Exception as e:
                self.session.log.info('[mgmt-agent] failed to return device_id')
                self.publishLogs('device_id', 'failed to return device_id {}'.format(e))
                raise

        def docker_service_rm(info):
            for s in self.client.services.list():
                if (s.name == info['container_name']):
                    s.remove()
                    return True
            raise Exception("service does not exists")

        def docker_login(username = 'device'):
            try:
                print('[mgmt-agent] trying to login with username: "{0}", password: "{1}"'.format(username, device_secret))
                # Login credentials are stored in memory of the client, so both clients are required to login
                resp = self.client.login(username, password=device_secret, reauth=True, registry=docker_registry_url[:-1])
                resp_low_level_api = self.api_client.login(username, password=device_secret, reauth=True, registry=docker_registry_url[:-1])
                pprint('[mgmt-agent] login client' + str(resp))
                pprint('[mgmt-agent] login Low Level Api client' + str(resp_low_level_api))
            except Exception as e:
                error_message = '{}'.format(e)
                self.session.log.error(
                    '[mgmt-agent] docker_login: ' + error_message)
                self.publishLogs('docker_login', error_message)
                # raise

        ##################################################
        def docker_run(info):
            pprint('[mgmt-agent] calling docker_run' + str(info))

            if 'image_name' not in info or info['image_name'] is None:
                raise Exception('Missing image name. Given: ' + str(info))

            if 'container_name' not in info or info['container_name'] is None:
                raise Exception('Missing container name. Given: ' + str(info))

            self.publishLogs('docker_run', 'preparing to run {}'.format(info))

            caller_id = info.get('caller_authid', 'device')
            docker_login(caller_id)
            try:
                docker_remove_container(info, False)
            except Exception as e:
                self.session.log.info('[mgmt-agent] warning: failed to remove container before run: {}'.format(e))
                return {"success": True, "message": "Container already running"}

            container_name = str(info['container_name'])
            env_array = prepare_environment_variables(info['environment'])
            restart_policy = prepare_restart_policy(info['stage'])
            volumes = prepare_volumes(info['stage'], info['app_name'])

            try:
                image_name = docker_registry_url + docker_main_repository + info['image_name'].lower()
                reactor.callFromThread(self.session.call, 'reswarm.logs.' + self.id + '.notify_start_container', container_name)
                self.session.log.info('[mgmt-agent] now running container {}'.format(container_name))
                self.client.containers.run(
                    image_name,
                    name=container_name,
                    # to distinguish run containers from build containers
                    labels={'real': 'True'},
                    detach=False,
                    tty=True,
                    devices=['/dev:/dev'],
                    cap_add=["ALL"],
                    # cap_drop=["NET_ADMIN"],
                    privileged=True,
                    environment=env_array,
                    restart_policy=restart_policy,
                    volumes=volumes,
                    network_mode='host'
                )
            except docker.errors.ContainerError as e:
                error_message = 'The container abruptly exited.'
                self.session.log.info('[mgmt-agent] docker_run container error: ' + str(e))
                reactor.callFromThread(self.session.publish, 'reswarm.logs.' + self.id + '.' + container_name, error_message)
            except docker.errors.ImageNotFound as e:
                error_message = 'Image was not found, please try removing and running the app again.'
                self.session.log.info('[mgmt-agent] docker_run image not found error: ' + str(e))
                reactor.callFromThread(self.session.publish, 'reswarm.logs.' + self.id + '.' + container_name, error_message)
            except Exception as e:
                error_message = str(e)
                self.session.log.info('[mgmt-agent] docker_run error: ' + error_message)
                if 'is already in use by container' in error_message:
                    return docker_run(info)
                if 'not found: manifest unknown' in error_message:
                    error_message = 'Could not find a valid image of this app, please try to build the image again'
                if 'No such container' in error_message or 'container which is dead' in error_message:
                    error_message = 'The app has been stopped forcefully'

                reactor.callFromThread(self.session.publish, 'reswarm.logs.' + self.id + '.' + container_name, error_message)

                raise Exception(error_message)

            # Don't remove the container here to be able to look at the containers log history.
            # The container is anyway removed on resetart of the container (see above)
            self.session.log.info('[mgmt-agent] docker_run ended')
            return "EXECUTION_END"

        def prepare_environment_variables(_environment):
            default_env_vars = {
                "SWARM_KEY": self.swarm_key,
                "DEVICE_NAME": self.device_name,
                "DEVICE_SERIAL_NUMBER": self.id
            }
            if not _environment or _environment is None:
                return default_env_vars # continue with default

            _env = {}
            # reduce the environment dict representation
            for k, v in (_environment or {}).items():
                _env[k] = v["value"]
            _env = { **_env, **default_env_vars}
            env_array = [e + '=' + str(_env[e]) for e in _env]
            return env_array

        def prepare_volumes(stage, app_name):
            volumes = {
                "/home/pirate/data/APP/" + stage + "/" + app_name + "": {"bind": "/data/", "mode": "rw"},
                "/home/pirate/data/shared/": {"bind": "/shared/", "mode": "rw"},
                "/var/run/docker.sock": {"bind": "/var/run/docker.sock", "mode": "ro"},
                "/boot": {"bind": "/boot", "mode": "rw"}
            }

            if os.path.exists("/sys/bus/w1/devices"):
                volumes["/sys/bus/w1/devices"] = {
                    "bind": "/sys/bus/w1/devices", "mode": "rw"}
            return volumes

        def prepare_restart_policy(stage):
            restart_policy = {"Name": "unless-stopped"}
            if stage == 'DEV':
                restart_policy = {"Name": "no"}
            return restart_policy

        def docker_prune_volumes():
            self.session.log.info('[mgmt-agent] calling docker prune volumes')
            try:
                pruned_volumes = self.client.volumes.prune()
                pprint('[mgmt-agent]' + str(pruned_volumes))
                return pruned_volumes
            except Exception as e:
                error_message = str(e)
                self.session.log.error('[mgmt-agent] error while pruning volumes: {}'.format(error_message))
                raise Exception(error_message)

        def docker_prune_containers():
            self.session.log.info('[mgmt-agent] calling docker prune containers')
            try:
                pruned_containers = self.client.containers.prune()
                pprint('[mgmt-agent]' + str(pruned_containers))
                return pruned_containers
            except Exception as e:
                error_message = str(e)
                self.session.log.error('[mgmt-agent] error while pruning containers: {}'.format(error_message))
                raise Exception(error_message)

        def docker_prune_networks():
            self.session.log.info('[mgmt-agent] calling docker prune networks')
            try:
                pruned_networks = self.client.networks.prune()
                pprint('[mgmt-agent]' + str(pruned_networks))
                return pruned_networks
            except Exception as e:
                error_message = str(e)
                self.session.log.error('[mgmt-agent] error while pruning networks: {}'.format(error_message))
                raise Exception(error_message)

        def docker_prune_images():
            self.session.log.info('[mgmt-agent] calling docker prune images')
            try:
                pruned_images = self.client.images.prune()
                pprint('[mgmt-agent]' + str(pruned_images))
                return pruned_images
            except Exception as e:
                error_message = str(e)
                self.session.log.error('[mgmt-agent] error while pruning images: {}'.format(error_message))
                raise Exception(error_message)

        def docker_prune_all():
            pprint('[mgmt-agent] prune all')
            try:
                docker_prune_volumes()
                docker_prune_containers()
                docker_prune_networks()
                docker_prune_images()
                return { 'success': True }
            except Exception as e:
                error_message = str(e)
                self.session.log.error('[mgmt-agent] error while pruning all: {}'.format(error_message))
                raise Exception(error_message)

        def docker_remove_container(info, force=True, show_message=True):
            try:
                container_name = info.get('container_name')
                self.session.log.info('[mgmt-agent] attempting to remove container with name: {}'.format(container_name))
                if container_name is None:
                    raise Exception('[mgmt-agent] Container name is missing from docker_remove_container request')
                self.api_client.remove_container(container_name, v=True, force=force)
                message = 'Successfully removed the container: {}'.format(container_name)
                if show_message:
                    reactor.callFromThread(self.session.publish, 'reswarm.logs.' + self.id + '.' + container_name, { 'type': 'build', 'chunk': message })
            except Exception as e:
                error_message = str(e)
                if '404 Client Error: Not Found' in error_message:
                    return {'success': True, 'message': 'Container not found' }
                if force == False and '409 Client Error: Conflict ("You cannot remove a running container' in error_message:
                    return {'success': True, 'message': 'Container still running and not force stopped' }
                if force == False and '409 Client Error: Conflict ("You cannot remove a restarting container' in error_message:
                    self.session.log.info('[mgmt-agent] container is restarting, retrying removal with force..')
                    return docker_remove_container(info, force=True, show_message=False)
                self.session.log.error('[mgmt-agent] Failed to stop Docker container: {}'.format(error_message))
                reactor.callFromThread(self.session.publish, 'reswarm.logs.' + self.id + '.' + container_name, { 'type': 'build', 'chunk': error_message })
                raise Exception(error_message)

        def docker_remove_image(info):
            pprint('[mgmt-agent] remove image')
            image_name = docker_registry_url + docker_main_repository + info['image_name'].lower()
            try:
                self.client.images.remove(image_name, force=True)
                return { 'success': True }
            except Exception as e:
                error_message = str(e)
                self.session.log.error('[mgmt-agent] Failed to remove Docker image: {}'.format(str(e)))
                if error_message.startswith('404 Client Error: Not Found'):
                    return { 'success': True, 'message': 'Image not found' }
                else:
                    reactor.callFromThread(self.session.publish, 'reswarm.logs.' + self.id + '.' + info['container_name'], { 'type': 'build', 'chunk': error_message })
                    raise Exception(error_message)

        def docker_pull(info):
            if info.get('image_name') is None:
                raise Exception('Missing image name. Given data: ' + str(info))

            if info.get('container_name') is None:
                ar = info.get('image_name').lower().split('_')
                del ar[1]
                container_name = '_'.join(ar).split(':')[0]
            else:
                container_name = info.get('container_name').lower()

            caller_id = info.get('caller_authid', 'device')
            topic = 'reswarm.logs.' + self.id + '.' + container_name
            image_name = docker_registry_url + docker_main_repository + info.get('image_name').lower()
            try:
                docker_login(caller_id)
                pprint('[mgmt-agent] Pulling image: ' + image_name)
                logs = self.api_client.pull(image_name, stream=True, decode=True)
                for chunk in logs:
                    self.session.log.info("{data!r}", data=chunk)
                    if 'error' in chunk:
                        raise Exception(chunk.get('error'))
                    reactor.callFromThread(self.session.publish, topic, { 'type': 'build', 'chunk': chunk })
                return { 'success': True }
            except Exception as e:
                error_message = str(e)
                self.session.log.error('[mgmt-agent] Failed to pull Docker image: {}'.format(error_message))
                reactor.callFromThread(self.session.publish, topic, { 'type': 'build', 'chunk': error_message })
                raise Exception(error_message)


        def docker_ps():
            pprint('[mgmt-agent] docker ps')
            res = []
            for c in self.client.containers.list(all=True):
                res.append({
                    'name': c.name,
                    'status': c.status,
                    'id': c.id,
                    'image': c.image.tags,
                    'image_id': c.image.id,
                    'attrs': c.attrs
                })
            return res

        def docker_images():
            pprint('[mgmt-agent] docker images')
            res = []
            for i in self.client.images.list():
                res.append({
                    'labels': i.labels,
                    'short_id': i.short_id,
                    'tags': i.tags,
                    'attrs': i.attrs
                })
            return res

        def docker_logs(info, tail=500):
            container_name = info.get('container_name')
            if container_name is None:
                raise Exception('Missing container name. Given data: ' + str(info))

            pprint('[mgmt-agent] docker_logs', container_name, tail)
            for c in self.client.containers.list(all=True):
                if (c.name == info['container_name']):
                    res = c.logs(tail=tail)
                    reactor.callFromThread(self.session.publish, 'reswarm.logs.' + self.id + '.' + container_name, res.decode("utf-8"))
                    return res.decode("utf-8")
            raise Exception("container is not running")

        def docker_stats():
            res = []
            for c in self.client.containers.list():
                stats = c.stats(stream=False)
                res.append(stats)
                pprint(stats)
                # self.session.log.info('[mgmt-agent] Docker stats' +  str(stats))
                # self.publishLogs('docker_stats', str(stats));
            return res

        def docker_build(info):
            self.session.log.info('[mgmt-agent] Docker build with params {}'.format(info))

            app_type = info.get('app_type', 'APP')
            self.session.log.info('[mgmt-agent] Name ' + docker_registry_url + info['name'] + app_type)

            app_local_path = "/home/pirate/" + app_type + "/" + info['name']

            image_name = info.get('image_name').lower()

            container_name = info.get('container_name')
            account_id = info.get('account_id')
            build_name = '{}_{}'.format(account_id, container_name)

            full_image_name = docker_registry_url + docker_main_repository + image_name
            self.session.log.info('[mgmt-agent] build with tag: ' + full_image_name + ' on path ' + app_local_path)
            topic = 'reswarm.logs.' + self.id + '.' + container_name
            docker_file_path = app_local_path + '/Dockerfile' # prevents name 'docker_file_path' is not defined error
            squash_image = info.get('squash', False)
            caller_id = info.get('caller_authid', 'device')

            # cancel any running builds of this app
            try:
                docker_remove_container(info, True)
            except Exception as e:
                error_message = str(e)
                self.session.log.info('[mgmt-agent] {}'.format(error_message))

            try:
                docker_login(caller_id)
                build_stream = threadsafe_iter(self.api_client.build(decode=True, path=app_local_path, tag=full_image_name, squash=squash_image, dockerfile=docker_file_path, forcerm=True))
                build_stream_dict[build_name] = build_stream

                self.session.log.info('[mgmt-agent] now publish build logs of container ' + container_name)
                for chunk in build_stream:
                    self.session.log.info("{data!r}", data=chunk)
                    if 'error' in chunk:
                        raise Exception(chunk.get('error'))
                    if 'errorDetail' in chunk:
                        raise Exception(chunk.get('message'))
                    reactor.callFromThread(self.session.publish, topic, { 'type': 'build', 'chunk': chunk })
                self.session.log.info('[mgmt-agent] build stream ended.')

                if not chunk.get('stream', '').startswith('Successfully tagged'):
                    error_message = str(chunk.get('stream', ''))
                    self.session.log.error('[mgmt-agent] Docker build failed on tag: {}'.format(error_message))
                    raise Exception('Stream ended abruptly without success')

                build_completed_stream = build_stream_dict.get(build_name)
                if build_completed_stream is not None:
                    build_completed_stream.cancel()
                    build_stream_dict.pop(build_name)

                return { 'success': True }
            except Exception as e:
                error_message = str(e)
                self.session.log.error('[mgmt-agent] Docker build failed: {}'.format(error_message))
                reactor.callFromThread(self.session.publish, 'reswarm.logs.' + self.id + '.' + container_name, { 'type': 'build', 'chunk': error_message })
                raise Exception(error_message)

        def docker_build_cancel(info):
            self.session.log.info('[mgmt-agent] stopping build container')
            try:
                container_name = info.get('container_name')
                account_id = info.get('account_id')
                app_name = info.get('name', container_name)

                self.session.log.info('[mgmt-agent] container_name: {}, account_id: {}'.format(container_name, account_id))
                if container_name is None or account_id is None:
                    raise Exception('The container name and the account id is required to cancel a build')

                build_name = '{}_{}'.format(account_id, container_name)
                build_stream = build_stream_dict.get(build_name)

                if build_stream is None:
                    raise Exception('No active build stream was found')

                self.session.log.info('Closing build stream with image name: {}'.format(build_name))
                build_stream.cancel()
                build_stream_dict.pop(build_name)

                success_message = 'Successfully stopped build for {}'.format(app_name)
                reactor.callFromThread(self.session.publish, 'reswarm.logs.' + self.id + '.' + container_name, { 'type': 'build', 'chunk': success_message })
                return { 'success': True }
            except Exception as e:
                error_message = str(e)
                self.session.log.error('[mgmt-agent] Failed to cancel Docker build: {}'.format(error_message))
                if 'No active build stream' not in error_message:
                    reactor.callFromThread(self.session.publish, 'reswarm.logs.' + self.id + '.' + container_name, { 'type': 'build', 'chunk': error_message })
                raise Exception(error_message)

        def docker_tag(info):
            caller_id = info.get('caller_authid', 'device')

            docker_login(caller_id)
            old_image_name = docker_registry_url + docker_main_repository + info.get('image_name').lower()
            new_image_name_tagless = docker_registry_url + docker_main_repository + info.get('new_image_name').split(':')[0]
            tag = info.get('version').lower()

            try:
                self.session.log.info("[mgmt-agent] tagging " + old_image_name + " " + new_image_name_tagless + " " + tag)
                successful = self.api_client.tag(old_image_name, new_image_name_tagless, tag)
                return { 'success': successful }
            except Exception as e: # https://docker-py.readthedocs.io/en/stable/api.html
                error_message = str(e)
                self.session.log.info('[mgmt-agent] Docker tag failed: {}'.format(error_message))
                reactor.callFromThread(self.session.publish, 'reswarm.logs.' + self.id + '.' + info['container_name'], { 'type': 'build', 'chunk': error_message })
                raise Exception(error_message)


        def docker_push(info):
            image_name = info.get('image_name').lower()
            container_name = info.get('container_name').lower()
            caller_id = info.get('caller_authid', 'device')

            docker_login(caller_id)
            try:
                pprint('[mgmt-agent] attempting to push image ' + image_name + ' to ' + docker_registry_url)
                logs = self.client.images.push(
                    repository=docker_registry_url + docker_main_repository + image_name,
                    stream=True,
                    decode=True
                )

                # Sample error chunk
                # {'error': 'denied: requested access to the resource is denied',
                # 'errorDetail': {'message': 'denied: requested access to the resource is denied'}}

                for chunk in logs:
                    self.session.log.info("{data!r}", data=chunk)
                    if 'error' in chunk:
                        raise Exception(chunk.get('error'))
                    reactor.callFromThread(self.session.publish, 'reswarm.logs.' + self.id + '.' + container_name, chunk)
                return { 'success': True }
            except Exception as e:
                error_message = str(e)
                self.session.log.info('Docker push failed: {}'.format(error_message))
                reactor.callFromThread(self.session.publish, 'reswarm.logs.' + self.id + '.' + info['container_name'], { 'type': 'build', 'chunk': error_message })
                raise Exception(error_message)

        # utility wrapper to run docker api in threads
        def runInThread(func, function_name):
            @inlineCallbacks
            def wrapper(*args):
                try:
                    pprint('[mgmt-agent] function ' + function_name)
                    result = yield threads.deferToThread(func, *args)
                except Exception as e:
                    error_message = '{}'.format(e)
                    pprint('[mgmt-agent] Device thread exception: ' + function_name + ' ' + error_message)
                    raise
                return result
            return wrapper

        ##################################################
        @inlineCallbacks
        def agent_update():
            pprint('[mgmt-agent] UPDATING AGENT...')
            arch = 'x86' if platform.machine() == 'x86_64' else 'arm'
            yield threads.deferToThread(docker_pull, {'image_name': docker_registry_url + docker_main_repository + arch + '_svc_mgmt_agent:latest', 'container_name': 'svc_mgmt_agent'})
            agent_restart()

        def agent_restart():
            self.session.leave()

        @inlineCallbacks
        def readme():
            self.session.log.info('[mgmt-agent] get readme')
            device_info_store = yield open('README.md', "r")
            message = device_info_store.read()
            returnValue(message)

        def untarApp(arch, path):
            try:
                pprint('[mgmt-agent] trying to untar file: ' + arch)
                tarf = tarfile.open(arch, "r:gz")
                tarf.extractall(path)
                tarf.close()
                os.remove(arch)
                pprint('[mgmt-agent] untar successfully')
            except Exception as e:
                self.session.log.error('[mgmt-agent] Error while untar data: {}'.format(e))
                # os.remove(arch)

        def _write_data(chunk, app_type, filename, container_name, total):
            # pprint('[mgmt-agent] _write_data')
            app_type = 'APP'
            archive = '/home/pirate/' + app_type + '/' + filename
            path = archive.split('.')[0]
            shutil.rmtree(path, ignore_errors=True)
            try:
                xfile = open(archive, 'ab+')
                if chunk == 'BEGIN':
                    pprint('[mgmt-agent] write_data: BEGIN writing file ' + filename)
                    pprint('[mgmt-agent] write_data: Total: {}'.format(total))
                    os.remove(archive)
                elif chunk == 'END':
                    pprint('[mgmt-agent] write_data: END ' + filename)
                    xfile.close()
                    pprint('[mgmt-agent] write_data: file written ' + filename)
                    untarApp(archive, path)
                else:
                    pprint('[mgmt-agent] write_data: write .')
                    xfile.write(bytes.fromhex(chunk))
                    pprint('[mgmt-agent] write_data: total ' + str(xfile.tell()))
                    xfile.seek(0, 2)
                    pprint('[mgmt-agent] write_data: total2 ' + str(xfile.tell()))
            except Exception as e:
                pprint('[mgmt-agent] error on write_data: {}'.format(e))
            return True

        def docker_start_app_logs():
            self.session.log.info('[mgmt-agent] docker starting svc_app_logs service')
            if (os.environ['ENV'] == 'DEV'):
                self.session.log.info('[mgmt-agent] DEVELOPMENT MODE. PLEASE RUN make build-services TO START THE LOGGER SERVICE')
                return
            try:
                docker_remove_container({'container_name': 'svc_app_logs'})
            except Exception as e:
                self.session.log.info('[mgmt-agent] Warning. Failed to stop svc_app_logs container: {}'.format(e))
            arch = 'x86' if platform.machine() == 'x86_64' else 'arm'

            docker_login()
            self.client.containers.run(
                'registry.reswarm.io/apps/' + arch + '_svc_app_logs',
                command=[],
                name='svc_app_logs',
                detach=True,
                # to distinguish run containers from build containers
                labels={'real': 'True'},
                # network='reswarm_network',
                network_mode='host',
                environment=[
                    "SERIAL_NUMBER={}".format(self.id),
                    "ENV={}".format(os.environ.get('ENV', 'PROD')),
                    "DEVICE_KEY={}".format(self.device_key),
                    "SWARM_KEY={}".format(self.swarm_key),
                    "DEVICE_SECRET={}".format(device_secret),
                    "DEVICE_ENDPOINT_URL={}".format(self.device_endpoint_url),
                ],
                restart_policy={"Name": "always"},
                volumes={"/home/pirate/config": {"bind": "/app/config", "mode": "rw"},
                         "/var/run/docker.sock": {"bind": "/var/run/docker.sock", "mode": "ro"}}
            )
            self.session.log.info('[mgmt-agent] docker started svc_app_logs service')

        def add_wifi(config):
            self.session.log.info('[mgmt-agent] add wifi')
            ssid = config.get('ssid')
            config_id = str(uuid.uuid4())
            password = config.get('password')
            priority = config.get('priority')

            if ssid is None or password is None or priority is None:
                raise Exception('Missing parameters; ssid: {}, password: {}, priority: {}'.format(ssid, password, priority))

            try:
                remove_wifi(config)
            except Exception as e:
                error_msg = str(e)
                self.session.log.info('[mgmt-agent] {}'.format(error_msg))

            con = '''
                # WPA2: {}
                country=DE
                ctrl_interface=DIR=/var/run/wpa_supplicant GROUP=netdev
                update_config=1
                network={{
                    id_str="{}"
                    ssid="{}"
                    psk="{}"
                    priority={}
                    proto=RSN
                    key_mgmt=WPA-PSK
                    pairwise=CCMP
                    auth_alg=OPEN
                }}
            '''.format(ssid, config_id, ssid, password, priority)

            file = open("/etc/wpa_supplicant/wpa_supplicant.conf", "a+")
            file.write(con)
            file.close()
            return con

        def remove_wifi(config):
            self.session.log.info('[mgmt-agent] remove wifi')
            file = open("/etc/wpa_supplicant/wpa_supplicant.conf", "r")
            wlan_config_lines = file.read().split('# WPA2:')

            name = config.get('ssid')

            if name is None:
                raise Exception('missing ssid parameter in request')

            index = -1

            for i in range(len(wlan_config_lines)):
                line = wlan_config_lines[i]
                if name in line.strip():
                    index = i

            if index == -1:
                raise Exception('wlan entry was not found in wpa config')

            wlan_config_lines.pop(index)

            wlan_config_str = '# WPA2:'.join(wlan_config_lines)

            file.close()
            file = open("/etc/wpa_supplicant/wpa_supplicant.conf", "w")
            file.write(wlan_config_str)
            return wlan_config_lines

        def toJSON(obj):
            return json.dumps(obj, default=lambda o: o.__dict__, sort_keys=True, indent=4)

        @inlineCallbacks
        def restart_wifi():
            self.session.log.info('[mgmt-agent] restart wifi')
            yield self.sys_cmd('ifconfig wlan0 down')
            yield sleep(1)
            yield self.sys_cmd('ifconfig wlan0 up')
            return 'ok'

        def toDict(somelist):
            _list = list(somelist)
            return { x[0]:x for x in _list}

        def scan_wifi():
            self.session.log.info('[mgmt-agent] scan wifi...')
            wifis = []
            cells = []
            interface = 'wlan0'
            if (os.environ['ENV'] != 'DEV'):
                try:
                    cells = Cell.all(interface)
                except Exception as e:
                    error_message = str(e)
                    self.session.log.error('[mgmt-agent] {}'.format(error_message))
            else:
                cells = os.system('networksetup -listpreferredwirelessnetworks en0')

            for cell in cells:
                pprint('[mgmt-agent] Cell.all {0}'.format(cell))
                wifi_details = {
                    "ssid": str(cell.ssid),
                    "mac": str(cell.address),
                    "signal": str(cell.signal),
                    "quality": str(cell.quality),
                    "frequency": str(cell.frequency),
                    "encryption": cell.encryption_type if cell.encrypted else None,
                    "channel": str(cell.channel),
                    "mode": str(cell.mode)
                    # "current": details[0] == currentWifi,
                    # "known_unused": details[0] in knownWifi,
                    # "unknown": (details[0] != currentWifi and details[0] not in knownWifi)
                }
                wifis.append(wifi_details)

            pprint('[mgmt-agent] network {0}'.format(wifis))
            return wifis

        def get_wifi():
            pprint('[mgmt-agent] get saved and current wifi')
            interface = 'wlan0'
            current_wlan = ''
            saved_networks = []

            if (os.environ['ENV'] == 'DEV'):
                interface = 'en0'
                current_wlan = os.popen('networksetup -getairportnetwork en0')
                saved_networks.append(current_wlan.split(" ")[-1])
            else:
                iw_result = os.popen('iw dev ' + interface + ' info | grep ssid').read().strip()
                list_wifi = 'grep -oE "ssid=\\".*\\"" /etc/wpa_supplicant/wpa_supplicant.conf | cut -c6- | sed s/\\"//g'
                wpa = os.popen(list_wifi).read()

                if wpa:
                    parsed_wpa_list = [entry for entry in wpa.split('\n') if entry != ""]
                    saved_networks.extend(parsed_wpa_list)

                iw_split = iw_result.split(' ')
                if iw_result and len(iw_split) > 1:
                    current_wlan = iw_split[1]


                pprint('[mgmt-agent] current_wlan {}'.format(saved_networks))

            wifi_data = {'saved_networks': saved_networks, 'current_network': current_wlan }
            return wifi_data

        def write_to(path, content):
            if path is None:
                return "invalid path argument"
            if not os.path.exists(os.path.dirname(path)):
                os.makedirs(os.path.dirname(path))

            try:
                pprint('[mgmt-agent] writing to ' +  path + ' content ' + content)
                f = open(path, "w")
                f.write(content)
                f.close()
            except IOError as errno:
                print("[mgmt-agent] I/O error({0})".format(errno))

        def updater(info):
            """
                ``updater`` function
                ======================
                Execute arbritaries updates on the host machine.

                :Example:
                    $0.session.absession.call('re.mgmt.06a0bf96-a539-4d6a-8471-ac7adc67616e.updater',
                        [{
                            volumes: ['/etc', '/bin'],
                            update_script: 'ls -la /volumes/etc/'
                        }]
                        )
                    .then(console.log)

                Attention
                -------------------
                It is required that the volumes on the update script
                have a prefix /volumes
            """
            docker_remove_container({ 'container_name': 'reswarm_updater' })
            pprint('[mgmt-agent] Starting updater with config {}'.format(info))

            if 'volumes' not in info or info['volumes'] == '':
                volumes = []
            else:
                volumes = json.loads(info['volumes'])
            if 'update_script' not in info or info['update_script'] == '':
                return 'no update script found'
            else:
                update_script = json.loads(info['update_script'])

            now = datetime.datetime.utcnow().strftime("%Y%m%d_%H%M%S")
            base_path = "/home/pirate/SERVICE/reswarm_updater/"
            file_name = now + '.sh'

            write_to(base_path + file_name, update_script + "\nsleep 10")

            dockerfile = '''
                FROM busybox:latest
                COPY {} ./{}
                COPY Dockerfile ./Dockerfile
                RUN chmod +x {}
                RUN echo '##############  RESWARM UPDATER v{} ###############'
                CMD ["sh", "/{}"]
            '''.format(file_name, file_name, file_name, now, file_name)
            write_to(base_path + 'Dockerfile', dockerfile)

            docker_build({
                'app_type': 'SERVICE',
                'name': 'reswarm_updater',
                'image_name': 'reswarm_updater',
                'container_name': 'reswarm_updater'
            })

            volume = ""
            _volumes = {
                "/var/run/docker.sock": {"bind": "/var/run/docker.sock", "mode": "ro"}
            }

            for volume in volumes:
                if volume and isinstance(volume, str):
                    _volumes[volume] = {
                        "bind": "/volumes/" + volume, "mode": "rw"
                    }
            try:
                con1 = self.client.containers.run('eu.gcr.io/record-1283/reswarm_updater:latest',
                    name='reswarm_updater',
                    labels={'real': 'True'},
                    detach=True,
                    privileged=True,
                    tty=True,
                    cap_add=["ALL"],
                    environment=["DEVICE_SERIAL_NUMBER={}".format(self.id)],
                    volumes=_volumes,
                    network_mode='host'
                )
            except Exception as e:
                error_message = '{0}'.format(e)
                pprint('[mgmt-agent] Error starting updater ' + error_message)
                reactor.callFromThread(self.session.publish, 'reswarm.updates.' + self.id + '.reswarm_updater', error_message)
                raise
            return con1.name


        """
        ``ufw_status`` function
        ======================
        lists all current settings for the firewall.

        :Example:
            $0.session.absession.call('re.mgmt.06a0bf96-a539-4d6a-8471-ac7adc67616e.ufw_status',[])
                .then(console.log)
        """
        def ufw_status():
            pprint('[mgmt-agent] calling ufw_status')
            try:
                rules = pyufw.get_rules()
                status = pyufw.status()
                result = { **rules, **status }
                pprint('[mgmt-agent] ufw status ' + json.dumps(result))
                return result
            except Exception as e:
                error_message = '{0}'.format(e)
                pprint('[mgmt-agent] Error on firewall ' + error_message)
                reactor.callFromThread(self.session.publish, 'reswarm.network.' + self.id + '.ufw_status', error_message)
                raise
        """
        ``ufw_listening`` function
        ======================
        Returns an array of listening ports, applications and rules that apply.

        :Example:
            $0.session.absession.call('re.mgmt.06a0bf96-a539-4d6a-8471-ac7adc67616e.ufw_listening',[])
                .then(console.log)
        """
        def ufw_listening():
            pprint('[mgmt-agent] calling ufw_listening')
            try:
                result = pyufw.show_listening()
                pprint('[mgmt-agent] ufw listening ' + json.dumps(result))
                return result
            except Exception as e:
                error_message = '{0}'.format(e)
                pprint('[mgmt-agent] Error on firewall ' + error_message)
                reactor.callFromThread(self.session.publish, 'reswarm.network.' + self.id + '.ufw_listening', error_message)
                raise

        def apply_firewall(info):
            pprint('[mgmt-agent] calling apply_firewall {}'.format(info))
            try:
                if ('enabled' in info and 'allow' in info and 'deny' in info):
                    _enable = { 'enable': info["enabled"] }
                    enabled = ufw_enable(_enable)

                    _allow = {
                        'allow': True,
                        'services': info["allow"],
                        'dry_run': False
                    }
                    allow = ufw_allow(_allow)

                    _deny = {
                        'allow': False,
                        'services': info["deny"],
                        'dry_run': False
                    }
                    deny = ufw_allow(_deny)
                else:
                    enabled = 'invalid'
                    allow = 'invalid'
                    deny = 'invalid'

                result_str = "Enable: {}\nAllow: {}\n Deny: {}".format(enabled, allow, deny)
                pprint('[mgmt-agent] apply_firewall ' + result_str)
                return result_str
            except Exception as e:
                error_message = '{0}'.format(e)
                pprint('[mgmt-agent] Error on appling firewall ' + error_message)
                reactor.callFromThread(self.session.publish, 'reswarm.network.' + self.id + '.apply_firewall', error_message)
                raise

        """
        ``ufw_enable`` function
        ======================
        Enable or disable a firewall.

        It is also possible to use the option dry_run,
        which indicates the results of the command without actually making any changes.

        :Example:
            $0.session.absession.call(
                're.mgmt.c6aa09d3-3dbd-4830-a230-bf75cdd7f149.ufw_enable',
                [{ enable: true }]
            )
            .then(console.log)
        """
        def ufw_enable(info):
            print('[mgmt-agent] calling ufw_enable')
            self.session.log.info('{}'.format(info))

            if info['enable'] is False:
                pprint('[mgmt-agent] disabling ufw...')
                try:
                    pyufw.disable()
                except Exception as e:
                    try:
                        error_message = '{0}'.format(e)
                        self.session.log.error('[mgmt-agent] Error disabling firewall ' + error_message)
                    except Exception as _e:
                        self.session.log.error('[mgmt-agent] Error disabling firewall')
                    reactor.callFromThread(self.session.publish, 'reswarm.network.' + self.id + '.ufw_enable', error_message)
            else:
                self.session.log.info('[mgmt-agent] enabling ufw...')
                try:
                    pyufw.enable()
                except Exception as e:
                    try:
                        error_message = '{0}'.format(e)
                        self.session.log.error('[mgmt-agent] Error enabling firewall ' + error_message)
                    except Exception as _e:
                        self.session.log.error('[mgmt-agent] Error enabling firewall')

                    reactor.callFromThread(self.session.publish, 'reswarm.network.' + self.id + '.ufw_enable', error_message)
            _status = pyufw.status()
            return _status


        """
        ``ufw_allow`` function
        ======================
        Allow or deny a port or service.

        It is also possible to use the option dry_run,
        which indicates the results of the command without actually making any changes.

        :Example:
            $0.session.absession.call('re.mgmt.06a0bf96-a539-4d6a-8471-ac7adc67616e.ufw_allow',
                [{
                    allow: false,
                    services: ['22/tcp', 'ssh']
                    dry_run: true
                }]
            )
            .then(console.log)
        """
        def ufw_allow(info):
            pprint('[mgmt-agent] calling ufw_allow {}'.format(info))
            allow = 'allow'

            if info["allow"] is False:
                allow = 'deny'
            if info["dry_run"] is True:
                dry_run = ' --dry-run'
            if info["services"] is None:
                raise 'No services found!'

            for _service in info["services"]:
                try:
                    pprint('[mgmt-agent] ufw ' + allow + ' ' + _service)
                    pyufw.add(allow + ' ' + _service)
                except Exception as e:
                    error_message = '{0}'.format(e)
                    pprint('[mgmt-agent] Error on firewall ' + error_message)
                    reactor.callFromThread(self.session.publish, 'reswarm.network.' + self.id + '.ufw_allow', error_message)
                    raise
            result = pyufw.get_rules()
            return result

        """
        ``ufw_reset`` function
        ======================
        Turn off firewall completely and delete all the rules.
        :Example:
            $0.session.absession.call('re.mgmt.06a0bf96-a539-4d6a-8471-ac7adc67616e.ufw_reset',[])
            .then(console.log)
        """
        def ufw_reset():
            pprint('[mgmt-agent] calling ufw_reset')
            try:
                result = pyufw.reset()
                pprint('[mgmt-agent] ufw reset defaults: ' + json.dumps(result))
                return result

            except Exception as e:
                error_message = '{0}'.format(e)
                pprint('[mgmt-agent] Error reseting firewall rules: ' + error_message)
                reactor.callFromThread(self.session.publish, 'reswarm.network.' + self.id + '.ufw_reset', error_message)
                raise

        @inlineCallbacks
        def registerAll():
            pprint('[mgmt-agent] register ---- APPS')
            reg3 = yield self.registerCatch(is_running, u're.mgmt.' + self.id + '.is_running')
            # reg3 = yield self.registerCatch(runInThread(publish_app, "publish_app"), u're.mgmt.' + self.id + '.publish_app')
            reg3 = yield self.registerCatch(runInThread(_write_data, "_write_data"), u're.mgmt.' + self.id + '.write_data')
            reg3 = yield self.registerCatch(runInThread(docker_remove_image, "docker_remove_image"), u're.mgmt.' + self.id + '.docker_remove_image')
            reg3 = yield self.registerCatch(runInThread(docker_tag, "docker_tag"), u're.mgmt.' + self.id + '.docker_tag')
            reg3 = yield self.registerCatch(runInThread(docker_remove_container, "docker_remove_container"), u're.mgmt.' + self.id + '.docker_remove_container')

            pprint('[mgmt-agent] register ---- CONFIG')
            reg3 = yield self.registerCatch(readme, u're.mgmt.' + self.id + '.readme')
            reg3 = yield self.registerCatch(runInThread(updater, "updater"), u're.mgmt.' + self.id + '.updater')

            pprint('[mgmt-agent] register ---- DEVICE START, STOP, UPDATE')
            reg3 = yield self.registerCatch(agent_update, u're.mgmt.' + self.id + '.agent_update')
            reg3 = yield self.registerCatch(self.system_reboot, u're.mgmt.' + self.id + '.system_reboot')
            reg3 = yield self.registerCatch(agent_restart, u're.mgmt.' + self.id + '.agent_restart')
            reg3 = yield self.registerCatch(device_handshake, u're.mgmt.' + self.id + '.device_handshake')

            pprint('[mgmt-agent] register ---- FIREWALL')
            reg = yield self.registerCatch(runInThread(apply_firewall, 'apply_firewall'), u're.mgmt.' + self.id + '.apply_firewall')
            reg = yield self.registerCatch(runInThread(ufw_enable, 'ufw_enable'), u're.mgmt.' + self.id + '.ufw_enable')
            reg = yield self.registerCatch(runInThread(ufw_status, 'ufw_status'), u're.mgmt.' + self.id + '.ufw_status')
            reg = yield self.registerCatch(runInThread(ufw_allow, 'ufw_allow'), u're.mgmt.' + self.id + '.ufw_allow')
            reg = yield self.registerCatch(runInThread(ufw_reset, 'ufw_reset'), u're.mgmt.' + self.id + '.ufw_reset')
            reg = yield self.registerCatch(runInThread(ufw_listening, 'ufw_listening'), u're.mgmt.' + self.id + '.ufw_listening')

            pprint('[mgmt-agent] register ---- WIFI')
            reg = yield self.registerCatch(runInThread(get_wifi, 'get_wifi'), u'svc_wifi.' + self.id + '.get_wifi')
            reg = yield self.registerCatch(runInThread(add_wifi, 'add_wifi'), u'svc_wifi.' + self.id + '.add_wifi')
            reg = yield self.registerCatch(runInThread(scan_wifi, 'scan_wifi'), u'svc_wifi.' + self.id + '.scan_wifi')
            reg = yield self.registerCatch(runInThread(remove_wifi, 'remove_wifi'), u'svc_wifi.' + self.id + '.remove_wifi')
            reg = yield self.registerCatch(runInThread(restart_wifi, 'restart_wifi'), u'svc_wifi.' + self.id + '.restart_wifi')

            pprint('[mgmt-agent] register ---- DOCKER STATS')
            reg3 = yield self.registerCatch(runInThread(docker_ps, "docker_ps"), u're.mgmt.' + self.id + '.docker_ps')
            reg3 = yield self.registerCatch(runInThread(docker_logs, "docker_logs"), u're.mgmt.' + self.id + '.docker_logs')
            reg3 = yield self.registerCatch(runInThread(docker_stats, "docker_stats"), u're.mgmt.' + self.id + '.docker_stats')
            reg3 = yield self.registerCatch(runInThread(docker_images, "docker_images"), u're.mgmt.' + self.id + '.docker_images')

            pprint('[mgmt-agent] register ---- DOCKER LIFECYCLE')
            reg3 = yield self.registerCatch(runInThread(docker_pull, "docker_pull"), u're.mgmt.' + self.id + '.docker_pull')
            # reg3 = yield self.registerCatch(runInThread(docker_stop, "docker_stop"), u're.mgmt.' + self.id + '.docker_stop')
            reg3 = yield self.registerCatch(runInThread(docker_run, "docker_run"), u're.mgmt.' + self.id + '.docker_run')
            # reg3 = yield self.registerCatch(runInThread(docker_start, "docker_start"), u're.mgmt.' + self.id + '.docker_start')
            reg3 = yield self.registerCatch(runInThread(docker_push, "docker_push"), u're.mgmt.' + self.id + '.docker_push')
            reg3 = yield self.registerCatch(runInThread(docker_build, "docker_build"), u're.mgmt.' + self.id + '.docker_build')
            reg3 = yield self.registerCatch(runInThread(docker_build_cancel, "docker_build_cancel"), u're.mgmt.' + self.id + '.docker_build_cancel')

            pprint('[mgmt-agent] register ---- DOCKER PRUNE')
            reg3 = yield self.registerCatch(runInThread(docker_prune_all, "docker_prune_all"), u're.mgmt.' + self.id + '.docker_prune_all')
            reg3 = yield self.registerCatch(runInThread(docker_prune_images, "docker_prune_images"), u're.mgmt.' + self.id + '.docker_prune_images')
            reg3 = yield self.registerCatch(runInThread(docker_prune_volumes, "docker_prune_volumes"), u're.mgmt.' + self.id + '.docker_prune_volumes')
            reg3 = yield self.registerCatch(runInThread(docker_prune_networks, "docker_prune_networks"), u're.mgmt.' + self.id + '.docker_prune_networks')
            reg3 = yield self.registerCatch(runInThread(docker_prune_containers, "docker_prune_containers"), u're.mgmt.' + self.id + '.docker_prune_containers')
            self.session.log.info('[mgmt-agent] Register functions done')

        pprint('[mgmt-agent] -------- register testament ---------------------')
        # docker connection
        self.client = docker.DockerClient(base_url='unix://var/run/docker.sock')
        self.api_client = docker.APIClient(base_url='unix://var/run/docker.sock')

        if(os.environ['ENV'] == 'DEV'):
            pprint('[mgmt-agent] DEVELOPMENT MODE. Connecting to local docker registry {}...'.format(docker_registry_url))

        try:
            res = yield self.session.call(
                u'wamp.session.add_testament',
                u'reswarm.api.testament_device', [{
                    u'tsp': datetime.datetime.utcnow().isoformat(),
                    u'device_key': self.device_key,
                    u'swarm_key': self.swarm_key
                }],
                {}
            )
            self.session.log.info('[mgmt-agent] Testament id {0}'.format(res))
        except Exception as e:
            self.session.log.error('[mgmt-agent] Error adding testament: {0}'.format(e))

        threads.deferToThread(docker_start_app_logs)

        yield registerAll()
        yield self.session.call(u'reswarm.containers.device_sync', self.device_key)

        try:
            docker_login()
        except Exception as e:
            self.session.log.error('[mgmt-agent] Error with docker login: {0}'.format(e))
            raise

    def system_reboot(self):
        # Standard sys_cmd don't have access to reboot, using systemctl reboot
        # on the pi it just work without the arg 0
        self.session.log.info('[mgmt-agent] SYSTEM REBOOTING NOW !')
        return os.system('systemctl reboot')

    def read_boot_config(self):
        try:
            device_boot_config_store = open('/boot/config.txt', "r")
            boot_config = device_boot_config_store.read()
            serialized = json.dumps(boot_config)
            pprint('[mgmt-agent] boot config {}'.format(boot_config))
            return boot_config
        except Exception as e:
            self.session.log.error('[mgmt-agent] Error reading boot config: {0}'.format(e))

    def write_boot_config(self, content):
        try:
            device_boot_config_store = open('/boot/config.txt', "w")
            device_boot_config_store.write(content)
            device_boot_config_store.close()
            return content
        except Exception as e:
            self.session.log.error('[mgmt-agent] Error writing boot config: {0}'.format(e))

    def read_cmdline(self):
        try:
            device_cmdline_store = open('/boot/cmdline.txt', "r")
            boot_cmdline = device_cmdline_store.read()
            return boot_cmdline
        except Exception as e:
            self.session.log.error('[mgmt-agent] Error reading cmdline.txt: {0}'.format(e))

    def write_cmdline(self, content):
        try:
            device_cmdline_store = open('/boot/cmdline.txt', "w")
            device_cmdline_store.write(content)
            device_cmdline_store.close()
            return content
        except Exception as e:
            self.session.log.error('[mgmt-agent] Error writing cmdnline.txt: {0}'.format(e))

    @inlineCallbacks
    def sys_cmd(self, stmt):
        try:
            res = yield getProcessOutput(u'/bin/bash', ('-c', stmt), os.environ)
            returnValue(res)
        except Exception as e:
            _err = unicodedata.normalize("NFKD", str(e))
            pprint('[mgmt-agent] sys_cmd: error on command: {0}'.format(_err))
            error = 'CMD error on statement {0}: stmt {1}'.format(stmt, _err)
            self.publishLogs(u'sys_cmd', error)
            pprint('[mgmt-agent] sys_cmd: ' + error)
            raise ValueError(error)

    @inlineCallbacks
    def registerCatch(self, fun, topic):
        try:
            yield self.session.register(fun, topic)
            self.session.log.info('[mgmt-agent] registered function {} on topic {}'.format(fun.__name__, topic))
        except ApplicationError as e:
            self.publishLogs(u'registerCatch', 'failed to register on topic ' + topic + 'Error: ' + str(e))
            yield self.session.disconnect()
            yield sleep(1)
            self.session.log.info('[mgmt-agent] waiting for {} to register on topic {}.'.format(fun.__name__, topic))

    @inlineCallbacks
    def onLeave(self, session, details):
        session.log.info('[mgmt-agent] session left: {}'.format(details))

        try:
            yield self.session.call(u'reswarm.devices.update_device', {
                'swarm_key': self.swarm_key,
                'device_key': self.device_key,
                'status': 'DISCONNECTED'
            })
        except:
            pass

        try:
            for build_name, build_stream in list(build_stream_dict.items()):
                build_stream.cancel()
                build_stream_dict.pop(build_name)
        except Exception as e:
            error_message = str(e)
            self.session.log.error('[mgmt-agent] Failed to cancel Docker build: {}'.format(error_message))

        self.session.disconnect()
        self.session = None

    def forceQuit(self):
        try:
            quit()
        except:
            print('[mgmt-agent] Could not quit')
        try:
            os._exit(1)
        except:
            print('[mgmt-agent] Could not exit')

        from twisted.internet import reactor
        reactor.stop()

        """Stop the reactor and join the reactor thread until it stops.
        Call this function in teardown at the module or package level to
        reset the twisted system after your tests. You *must* do this if
        you mix tests using these tools and tests using twisted.trial.
        """
        global _twisted_thread

        def stop_reactor():
            '''Helper for calling stop from withing the thread.'''
            reactor.stop()

        reactor.callFromThread(stop_reactor)
        for p in reactor.getDelayedCalls():
            if p.active():
                p.cancel()
        _twisted_thread = None

    def onDisconnect(self, session, was_clean):
        session.log.info('[mgmt-agent] transport disconnected')
        self.forceQuit()
        print('[mgmt-agent] Reactor Stopped')

    def onConnectfailure(self, session, was_clean):
        session.log.info('[mgmt-agent] disconnect on connectfailure')
        self.forceQuit()

        from twisted.internet import reactor
        reactor.stop()


if __name__ == '__main__':
    ENV = os.environ.get('ENV')

    if ENV == 'DEV':
        filename = inspect.getframeinfo(inspect.currentframe()).filename
        current_dir = '/app' #os.path.dirname(os.path.abspath(filename))
    else:
        current_dir = '/boot'

    pprint('[mgmt-agent] Platform: {} System: {}'.format(platform.machine(), platform.system()))
    pprint('[mgmt-agent] Crossbar Location {}: '.format(device_endpoint_url))
    pprint('[mgmt-agent] Current Directory: {}'.format(current_dir))

    from twisted.internet._sslverify import OpenSSLCertificateAuthorities
    from twisted.internet.ssl import CertificateOptions
    from OpenSSL import crypto, SSL
    cert_path = os.path.join(current_dir, 'client.cert.pem')
    key_path = os.path.join(current_dir, 'client.key.pem')

    # load client certificate and key
    # see autobahn example on github: /Users/marko/git/AutobahnPython/examples/twisted/wamp/pubsub/tls/backend_selfsigned.py
    cert_client = crypto.load_certificate(
        crypto.FILETYPE_PEM,
        six.u(open(cert_path, 'r').read())
    )

    with open(key_path, 'r') as f:
        client_key_data = f.read()
    key_client = crypto.load_privatekey(crypto.FILETYPE_PEM, client_key_data)

    # tell Twisted to use the client certificate
    ssl_options = CertificateOptions(privateKey=key_client, certificate=cert_client)
    # ...which we pass as "ssl=" to ApplicationRunner (passed to SSL4ClientEndpoint)

    parser = argparse.ArgumentParser()
    parser.add_argument('--router', type=six.text_type, default=device_endpoint_url, help=u'WAMP router URL.')  # Development
    parser.add_argument('--realm', type=six.text_type, default=u'realm1', help='WAMP router realm.')
    args = parser.parse_args()

    pprint('[mgmt-agent] Connection on: {}'.format(device_endpoint_url))

    is_tls = True if device_endpoint_url.split('://')[0] == 'wss' else False

    host_parts = device_endpoint_url.split('://')[1].split(':')
    if host_parts[0] is None:
        crossbar_host = 'cb.reswarm.io'
    else:
        crossbar_host = host_parts[0]

    if host_parts[1] is None:
        crossbar_port = 8080
    else:
        crossbar_port = host_parts[1]

    transport = {
        "type": "websocket",
        "url": args.router,
        "serializers": ['json'],
        'max_retries': -1,
        'initial_retry_delay': 1,
        'max_retry_delay': 4,
        'retry_delay_growth': 2,
        'retry_delay_jitter': 0.1,
        # you can set various websocket options here if you want
        "options": {
            "openHandshakeTimeout": 2000,
            "closeHandshakeTimeout": 1000,
            "echoCloseCodeReason": True,
            "utf8validateIncoming": False,
            "failByDrop": False,
            "autoPingInterval": 5 * 60,
            "autoPingTimeout": 60 * 60, # one hour because we experience websocket ping pong problems
            "autoPingSize": 8
            # 'auto_reconnect': True
        }
    }

    if is_tls is True:
        transport["endpoint"] = {
            "type": "tcp",
            "host": crossbar_host,
            "port": int(crossbar_port),
            "tls": ssl_options
        }

    component = Component(
        # you can configure multiple transports; here we use two different
        # transports which both exist in the demo router
        transports=[transport],
        # authentication can also be configured (this will only work on
        authentication={
            u"wampcra": {
                u'authid': '{}-{}'.format(s_key, d_key),
                u'secret': device_secret
            }
        },
        realm=args.realm
    )
    app = App(component)
    run([component], log_level='info')
