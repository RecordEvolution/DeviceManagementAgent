Make sure that you have Docker (version>19.*) installed on your device (Linux or MacOS).

Use following bash one liners to install REagent for your device (e.g. fill in architecture and OS)  with url https://storage.googleapis.com/re-agent/install/install_reagent.sh

- for Linux

``` wget <url to  install script> -v -O install.sh && sudo chmod +x ./install.sh && ./install.sh linux <architecture>; rm -rf install.sh ```

- for MacOS (darwin)

``` curl <url to install script> -o install.sh && sudo chmod +x ./install.sh && ./install.sh darwin <architecture> ; rm -rf install.sh ```

To start your device with the downloaded device file (.reswarm), run the following command:
 
```sudo reagent -update=false -config <your_device_file.reswarm>``` 

This will connect your device to the Record Evolution Platform. (For help: reagent -help)