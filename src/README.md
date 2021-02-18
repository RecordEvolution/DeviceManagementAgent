## Dev Setup for Reagent

## Step 1:

Flash your device with RESWARMOS
`https://storage.cloud.google.com/reswarmos/NOOS-0.0.5-raspberrypi4.img.zip`

## Step 2

After the flash process has successfully finished, open up the boot partition (vfat) labeled RESWARMOS containing the file /device-config.ini.

Enter your WiFi credentials, your username, your password and your hostname in the config.

Start your Pi

connect via:
`ssh <your-username>@<device-hostname>`


## Step 3

Ssh copy the reagent binary and the database scripts over to the device.

## Step 4

Go to your `git/RESWARM` directory and checkout to the `feat/deviceManagementAgentChanges` branch.

Go to the docker-compose.yml file and edit the following key of the `registry` entry:
```
REGISTRY_AUTH_TOKEN_REALM: https://192.168.178.36:5002/token
```

Replace `192.168.178.36` with the local IP of the machine running Reswarm (aka your laptop local ip)

Make sure to `make down-repods && make down && make repods && make build`

(aka just make sure that you get that branch running)

## Step 5

Create a new device in Reswarm and make sure to enter the correct WiFi credentials.

**Download and save the `.reswarm` file**

Open the `.reswarm` file and adjust the following keys:
```
  "device_endpoint_url": "wss://192.168.178.36:8080",
  "docker_registry_url": "192.168.178.36:5000/",
  "insecure-registries": "[\"192.168.178.36:5000\"]",
```

**NOTE:** make sure the `docker_registry_url` value ends with a **forward slash**

Replace `192.168.178.36` with the local IP of the machine running Reswarm (aka your laptop local ip)

After applying the changes copy the `.reswarm` file over to your device with scp.

## Final Step

Go to your home directory and run the following command:

`sudo ./reagent -config demo_demo_swarm_TestDevice.reswarm -debug`

Make sure to replace the `.reswarm` file with your `.reswarm` file.