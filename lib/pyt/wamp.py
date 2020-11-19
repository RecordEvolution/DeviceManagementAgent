# --------------------------------------------------------------------------- #

from autobahn.twisted.component import Component, run
from autobahn.wamp import auth
import OpenSSL # python3 -m pip install pyopenssl service_identity
from twisted.internet.ssl import CertificateOptions
import datetime

# read private key and certificate
with open('../go/deviceagent/key.pem','r') as fin :
    key_pem = fin.read()
with open('../go/deviceagent/cert.pem','r') as fin :
    cert_pem = fin.read()

# https://autobahn.readthedocs.io/en/latest/wamp/programming.html
demo = Component(
    transports=[{
        "type": "websocket",
        "url": "wss://cb.reswarm.io:8080",
        "endpoint": {
            "type": "tcp",
            "host": "cb.reswarm.io",
            "port": 8080,
            # https://twistedmatrix.com/documents/16.4.1/api/twisted.internet.ssl.CertificateOptions.html
            "tls": CertificateOptions(
                privateKey=OpenSSL.crypto.load_privatekey(OpenSSL.crypto.FILETYPE_PEM,key_pem),
                certificate=OpenSSL.crypto.load_certificate(OpenSSL.crypto.FILETYPE_PEM, cert_pem)
            )
        },
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
    }],
    realm="realm1",
    authentication={
        u"wampcra": {
            u'authid': '{}-{}'.format(44, 3285),
            u'secret': "CZ3amCyKMxLsauC5+vGTZw=="
        }
    },
)

@demo.on_join
async def joined(session,details) :

    print(session)
    print(details)

    # https://github.com/RecordEvolution/RESWARM/blob/master/backend/api/devices/devices.ts

    try:
        res = yield session.call(
            u'wamp.session.add_testament',
            u'reswarm.api.testament_device', [{
                u'tsp': datetime.datetime.utcnow().isoformat(),
                u'device_key': 3285,
                u'swarm_key': 44
            }],
            {}
        )
        print('[mgmt-agent] Testament id {0}'.format(res))
    except Exception as e:
        print('[mgmt-agent] Error adding testament: {0}'.format(e))

    try:
        session.register(is_running, u're.mgmt.' + '813e9e53-fe1f-4a27-a1bc-a97e8846a5a2' + '.is_running')
        print("procedure is_running registered")
    except Exception as e:
        print("could not register procedure: {0}".format(e))

    try:
        session.register(device_handshake, u're.mgmt.' + '813e9e53-fe1f-4a27-a1bc-a97e8846a5a2' + '.device_handshake')
        print("procedure device_handshake registered")
    except Exception as e:
        print("could not register procedure: {0}".format(e))

# @demo.register(u're.mgmt.' + '813e9e53-fe1f-4a27-a1bc-a97e8846a5a2' + '.is_running')
def is_running():
    print('is_running called...')
    return True

def device_handshake():
    print('device handshake called...')
    try:
        return {u'tsp': datetime.datetime.utcnow().isoformat(), u'id': '813e9e53-fe1f-4a27-a1bc-a97e8846a5a2'}
    except Exception as e:
        print("failed to return device_id")
        # self.session.log.info('[mgmt-agent] failed to return device_id')
        # self.publishLogs('device_id', 'failed to return device_id {}'.format(e))
        raise

# # 1. subscribe to a topic
# @demo.subscribe(u'realm1')
# def hello(msg):
#     print("Got hello: {}".format(msg))
#
# # 2. register a procedure for remote calling
# @demo.register(u're.mgmt.' + '3285' + '.docker_ps')
# def add2(x, y):
#     return x + y

# 3. after we've authenticated, run some code
# @demo.on_join
# async def joined(session, details):
#     # publish an event (won't go to "this" session by default)
#     await session.publish('realm1', 'Hello, world!')
#
#     # 4. call a remote procedure
#     result = await session.call('com.myapp.add2', 2, 3)
#     print("com.myapp.add2(2, 3) = {}".format(result))

if __name__ == "__main__" :

    print("starting WAMP client...")

    run([demo],log_level='debug')

# --------------------------------------------------------------------------- #
