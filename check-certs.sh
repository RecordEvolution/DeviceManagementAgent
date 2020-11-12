#!/bin/bash

# provide .pem file path or URL
certtocheck="$1"

if [[ -z "${certtocheck}" ]]; then
  echo "please provide a valid file/path or URL to be checked !" >&2
  exit 1
fi

echo "check ${certtocheck} as file or treat as URL ? (choose file/url)"
read -p "" inpt
if [[ "${inpt}" == "file" ]]; then
  echo "checking certificate '${certtocheck}'..."
  cat "${certtocheck}" | openssl x509 -noout -dates
  # sudo cat /etc/letsencrypt/live/orbifold.de/cert.pem | openssl x509 -noout -dates
elif [[ "${inpt}" == "url" ]]; then
  echo "to check the certificate of an URL please also provide the desired port!"
  read -p "" portnum
  echo "checking certificate of '${certtocheck}:${portnum}'... "
  echo "Q" | openssl s_client -connect ${certtocheck}:${portnum} -servername ${certtocheck} | openssl x509 -noout -dates
else
  echo "please choose either 'file' or 'url' !" >&2
  exit 1
fi
