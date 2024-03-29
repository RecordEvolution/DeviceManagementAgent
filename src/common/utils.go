package common

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"reagent/config"
	"reagent/messenger/topics"
	"reagent/release"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/rs/zerolog/log"
)

func BuildContainerName(stage Stage, appKey uint64, appName string) string {
	return strings.ToLower(fmt.Sprintf("%s_%d_%s", stage, appKey, appName))
}

func BuildComposeContainerName(stage Stage, appKey uint64, appName string) string {
	return strings.ToLower(fmt.Sprintf("%s_%d_%s_compose", stage, appKey, appName))
}

func BuildImageName(stage Stage, arch string, appKey uint64, appName string) string {
	return strings.ToLower(fmt.Sprintf("%s_%s_%d_%s", stage, arch, appKey, appName))
}

func BuildRegistryImageName(registryURL string, mainRepositoryName string, imageName string) string {
	return strings.ToLower(fmt.Sprintf("%s%s%s", registryURL, mainRepositoryName, imageName))
}

func BuildAgentUpdateProgress(serialNumber string) string {
	return fmt.Sprintf("%s.%s.%s", topicPrefix, serialNumber, topics.AgentProgress)
}

func BuildDownloadOSUpdateProgress(serialNumber string) string {
	return fmt.Sprintf("%s.%s.%s", topicPrefix, serialNumber, topics.DownloadOSUpdateProgress)
}

func BuildInstallOSUpdateProgress(serialNumber string) string {
	return fmt.Sprintf("%s.%s.%s", topicPrefix, serialNumber, topics.InstallOSUpdateProgress)
}

func BuildPerformOSUpdateProgress(serialNumber string) string {
	return fmt.Sprintf("%s.%s.%s", topicPrefix, serialNumber, topics.PerformOSUpdateProgress)
}

func BuildTunnelStateUpdate(serialNumber string) string {
	return fmt.Sprintf("%s.%s.%s/onreload", topicPrefix, serialNumber, topics.TunnelStateUpdate)
}

const topicPrefix = "re.mgmt"

func BuildLogTopic(serialNumber string, containerName string) string {
	return fmt.Sprintf("reswarm.logs.%s.%s", serialNumber, containerName)
}

func BuildExternalApiTopic(serialNumber string, topic string) string {
	return fmt.Sprintf("%s.%s.%s", topicPrefix, serialNumber, topic)
}

func BuildDockerBuildID(appKey uint64, appName string) string {
	return fmt.Sprintf("build_%d_%s", appKey, appName)
}

func BuildDockerPullID(appKey uint64, appName string) string {
	return fmt.Sprintf("pull_%d_%s", appKey, appName)
}

func BuildDockerPushID(appKey uint64, appName string) string {
	return fmt.Sprintf("push_%d_%s", appKey, appName)
}

func EscapeNewlineCharacters(input string) string {
	input = strings.ReplaceAll(input, "\n", "\\n")
	input = strings.ReplaceAll(input, "\t", "\\t")
	input = strings.ReplaceAll(input, "\r", "\\r")
	return input
}

func EnvironmentTemplateToStringArray(environmentsTemplateMap map[string]interface{}) []string {
	stringArray := make([]string, 0)

	for key, entry := range environmentsTemplateMap {
		value := entry.(map[string]interface{})["defaultValue"]
		if value != nil {
			stringArray = append(stringArray, fmt.Sprintf("%s=%s", key, EscapeNewlineCharacters(fmt.Sprint(value))))
		}
	}

	return stringArray
}

func ListDirectories(path string) ([]string, error) {
	folder, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer folder.Close()

	subfolders, err := folder.Readdir(-1)
	if err != nil {
		return nil, err
	}

	var directories []string
	for _, item := range subfolders {
		if item.IsDir() {
			directories = append(directories, item.Name())
		}
	}

	return directories, nil
}

func EnvironmentVarsToStringArray(environmentsMap map[string]interface{}) []string {
	stringArray := make([]string, 0)

	for key, entry := range environmentsMap {
		value := entry.(map[string]interface{})["value"]
		stringArray = append(stringArray, fmt.Sprintf("%s=%s", key, EscapeNewlineCharacters(fmt.Sprint(value))))
	}

	return stringArray
}

var StatusRegex = regexp.MustCompile(`\((.*?)\)`)

func ParseExitCodeFromContainerStatus(status string) (int64, error) {
	statusString := StatusRegex.FindString(status)
	exitCodeString := strings.TrimRight(strings.TrimLeft(statusString, "("), ")")
	exitCodeInt, err := strconv.ParseInt(exitCodeString, 10, 64)
	if err != nil {
		return -1, err
	}

	return exitCodeInt, nil
}

func GetRandomFreePort() (port int, err error) {
	var a *net.TCPAddr
	if a, err = net.ResolveTCPAddr("tcp", "localhost:0"); err == nil {
		var l *net.TCPListener
		if l, err = net.ListenTCP("tcp", a); err == nil {
			defer l.Close()
			return l.Addr().(*net.TCPAddr).Port, nil
		}
	}
	return
}

func GetFreePortFromStart(startPort int) (int, error) {
	for port := startPort; port <= 65535; port++ {
		addr := fmt.Sprintf(":%d", port)
		listener, err := net.Listen("tcp", addr)
		if err == nil {
			defer listener.Close()
			return listener.Addr().(*net.TCPAddr).Port, nil
		}
	}
	return 0, fmt.Errorf("no free port available")
}

// Ordinal gives you the input number in a rank/ordinal format.
//
// Ordinal(3) -> 3rd
func Ordinal(x uint) string {
	suffix := "th"
	switch x % 10 {
	case 1:
		if x%100 != 11 {
			suffix = "st"
		}
	case 2:
		if x%100 != 12 {
			suffix = "nd"
		}
	case 3:
		if x%100 != 13 {
			suffix = "rd"
		}
	}
	return strconv.Itoa(int(x)) + suffix
}

func ParseContainerName(containerName string) (Stage, uint64, string, error) {
	if containerName == "" {
		return "", 0, "", errors.New("container name is empty")
	}

	// cleanup container name
	if string([]rune(containerName)[0]) == "/" {
		// get index of the rune that == /
		_, i := utf8.DecodeRuneInString(containerName)
		// remove that rune from the string
		containerName = containerName[i:]
	}

	var stage Stage
	var appKey uint64
	var name string

	containerSplit := strings.Split(containerName, "_")
	if len(containerSplit) < 3 {
		return "", 0, "", errors.New("invalid container name")
	}

	if containerSplit[0] == "dev" {
		stage = DEV
	} else if containerSplit[0] == "prod" {
		stage = PROD
	} else if containerSplit[0] == "pub" {
		stage = DEV
	} else {
		stage = ""
	}

	parsedAppKey, err := strconv.ParseUint(containerSplit[1], 10, 64)
	if err != nil {
		return "", 0, "", err
	}
	appKey = parsedAppKey

	// also handles names like dev_6_net_data, aka 2 _'s at the end
	name = strings.Join(containerSplit[2:], "_")

	return stage, appKey, name, nil
}

func ParseComposeContainerName(containerName string) (Stage, uint64, string, error) {
	if containerName == "" {
		return "", 0, "", errors.New("container name is empty")
	}

	// cleanup container name
	if string([]rune(containerName)[0]) == "/" {
		// get index of the rune that == /
		_, i := utf8.DecodeRuneInString(containerName)
		// remove that rune from the string
		containerName = containerName[i:]
	}

	var stage Stage
	var appKey uint64
	var name string

	containerSplit := strings.Split(containerName, "_")
	if containerSplit[3] != "compose" {
		return "", 0, "", errors.New("invalid compose container name")
	}

	if containerSplit[0] == "dev" {
		stage = DEV
	} else if containerSplit[0] == "prod" {
		stage = PROD
	} else if containerSplit[0] == "pub" {
		stage = DEV
	} else {
		stage = ""
	}

	parsedAppKey, err := strconv.ParseUint(containerSplit[1], 10, 64)
	if err != nil {
		return "", 0, "", err
	}
	appKey = parsedAppKey

	name = containerSplit[2]

	return stage, appKey, name, nil
}

func (tp *TransitionPayload) initContainerData(appKey uint64, appName string, config *config.Config) {
	publishContainer := BuildContainerName("pub", appKey, appName)
	devContainerName := BuildContainerName(DEV, appKey, appName)
	prodContainerName := BuildContainerName(PROD, appKey, appName)

	_, arch, variant := release.GetSystemInfo()
	imageArchName := arch + variant

	devImageName := BuildImageName(DEV, imageArchName, appKey, appName)
	devRegImageName := BuildRegistryImageName(config.ReswarmConfig.DockerRegistryURL, config.ReswarmConfig.DockerMainRepository, devImageName)

	prodImageName := BuildImageName(PROD, imageArchName, appKey, appName)
	prodRegImageName := BuildRegistryImageName(config.ReswarmConfig.DockerRegistryURL, config.ReswarmConfig.DockerMainRepository, prodImageName)

	tp.PublishContainerName = publishContainer
	tp.ContainerName = StageBasedResult{
		Dev:  devContainerName,
		Prod: prodContainerName,
	}
	tp.ImageName = StageBasedResult{
		Dev:  devImageName,
		Prod: prodImageName,
	}
	tp.RegistryImageName = StageBasedResult{
		Dev:  devRegImageName,
		Prod: prodRegImageName,
	}
}

func PrettyPrintDebug(data interface{}) {
	pretty, err := PrettyFormat(data)
	if err != nil {
		pretty = fmt.Sprintf("%+v", pretty)
	}

	log.Debug().Msg(pretty)
}

func Min(a int64, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func Log(format string, stdout bool, logLevel string, val ...interface{}) {
	levelMsg := strings.ToUpper(logLevel)

	if stdout {
		args := make([]interface{}, 0)
		args = append(args, fmt.Sprintf("%s:", levelMsg))
		args = append(args, val...)

		if format == "" {
			fmt.Println(args)
			return
		} else {
			fmt.Printf(format, args)
			return
		}
	}

	event := log.Debug()
	switch logLevel {
	case "error":
		event = log.Error()
	case "info":
		event = log.Info()
	case "warning":
		event = log.Warn()
	}

	if format == "" {
		event.Msg(fmt.Sprint(val...))
		return
	} else {
		event.Msgf(format, val...)
		return
	}
}

func PrettyFormat(data interface{}) (string, error) {
	var p []byte
	//    var err := error
	p, err := json.MarshalIndent(data, "", "\t")
	if err != nil {
		return "", err
	}

	return string(p), nil
}
