#!/bin/bash

# define default archive name and path
arpa="${HOME}/Downloads/test-repo.tar.xz"

echo "generating test repository ${arpa}"

# get parent directory and name of repository folder
arpanam=$(basename ${arpa} | awk -F '.' '{print $1}')
arpadir=$(dirname ${arpa})
arpafol="${arpadir}/${arpanam}"

# create folder for respository
mkdir -pv "${arpafol}"

# define contents of Dockerfile
dockerfile=$(cat << 'EOF'

FROM ubuntu:latest

RUN apt-get update && apt-get upgrade -y

COPY ./start-app.sh ./
RUN chmod u+x ./start-app.sh

CMD ["./start-app.sh"]

EOF
)

# define contents of entry point script
scriptfile=$(cat << 'EOF'
#!/bin/bash

echo "Hello World!"

EOF
)

# generate Dockerfile and script in respository folder
echo "${dockerfile}" > ${arpafol}/Dockerfile
echo "${scriptfile}" > ${arpafol}/start-app.sh

# generate (compressed) archive from repository
tar -cvzf ${arpa} ${argafol}

# remove respository folder
rm -rvf ${arpa}
