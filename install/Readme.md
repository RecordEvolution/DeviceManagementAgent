Make sure that you have Docker (version>19.*) installed on your device (Linux or MacOS).

Use following bash one liner to install REagent for your device:

``` curl https://storage.googleapis.com/re-agent/install/install_reagent.sh -o install.sh && sudo chmod +x ./install.sh && ./install.sh; rm -rf install.sh ```

To start your device with the downloaded device file (.reswarm), run the following command:

```sudo reagent -update=false -config <your_device_file.reswarm>``` 

This will connect your device to the Record Evolution Platform. (For help: reagent -help)