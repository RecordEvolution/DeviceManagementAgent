const { exec } = require("child_process");
const args = Parse(process.argv);
const builderArch = process.arch === "x64" ? "amd64" : process.arch;

const flagMap = {
  cc: {
    5: "-march=armv5",
    6: "-march=armv6 -mfpu=vfp -mfloat-abi=hard",
    7: "-march=armv7-a -fPIC",
  },
};

let { arch, armv, bflags, cc, os, outputDir, v, ccgcc, ccgxx } = args;

if (!arch) arch = "arm";
if (!ccgcc && arch === "arm") {
  ccgcc = "arm-linux-gnueabihf-gcc";
}
if (!ccgxx && arch === "arm") {
  ccgxx = "arm-linux-gnueabihf-g++";
}
if (!outputDir) {
  outputDir = "build/";
}

if (outputDir.slice(-1) !== "/") {
  outputDir += "/";
}

if (!os) os = "linux";
if (!v) v = false;
if (!cc) cc = true;

const buildArgString = armv ? arch + "v" + armv : arch;
bflags = [`reagent/system.BuildArch=${buildArgString}`];

let options = `GOOS=${os} GOARCH=${arch} CGO_ENABLED=1`;
if (cc && ccgcc && builderArch !== arch) {
  options += ` CC=${ccgcc} `;
  if (armv) {
    options += ` CGO_CFLAGS="${flagMap.cc[armv]}" `;
  }
}

if (armv) {
  options += ` GOARM=${armv} `;
}

const buildFlagString = [...bflags.map((bf) => `-X '${bf}'`)].join(" ");

let outputName = `reagent-${os}-${buildArgString}`;
if (os === "windows") {
  outputName += ".exe";
}

const command = `${options} go build -v -a -o ${outputDir}${outputName} -ldflags "${buildFlagString} -extldflags=-static" .`;
console.log(`Building ${outputName}...`);
if (v) {
  console.log("Command:", command);
}

const buildProcess = exec(command);
buildProcess.stdout.on("data", function (data) {
  const stringData = data.toString();
  if (stringData.includes("\n")) {
    process.stdout.write(stringData);
  } else {
    console.log(stringData);
  }
});

buildProcess.stderr.on("data", function (data) {
  const stringData = data.toString();
  if (stringData.includes("\n")) {
    process.stdout.write(stringData);
  } else {
    console.log(stringData);
  }
});

buildProcess.on("error", function (data) {
  const stringData = data.toString();
  if (stringData.includes("\n")) {
    process.stdout.write(stringData);
  } else {
    console.log(stringData);
  }
});

buildProcess.on("exit", function (code) {
  if (code === 0) {
    console.log("Done!\n");
  } else {
    console.log("Exited with exit code:", code)
  }
});

function Parse(argv) {
  // Removing node/bin and called script name
  const ARGUMENT_SEPARATION_REGEX = /([^=\s]+)=?\s*(.*)/;
  argv = argv.slice(2);

  const parsedArgs = {};
  let argName, argValue;

  argv.forEach(function (arg) {
    // Separate argument for a key/value return
    arg = arg.match(ARGUMENT_SEPARATION_REGEX);
    arg.splice(0, 1);

    // Retrieve the argument name
    argName = arg[0];

    // Remove "--" or "-"
    if (argName.indexOf("-") === 0) {
      argName = argName.slice(argName.slice(0, 2).lastIndexOf("-") + 1);
    }

    // Parse argument value or set it to `true` if empty
    argValue =
      arg[1] !== ""
        ? parseFloat(arg[1]).toString() === arg[1]
          ? +arg[1]
          : arg[1]
        : true;

    parsedArgs[argName] = argValue;
  });

  return parsedArgs;
}
