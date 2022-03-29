package common

import (
	"encoding/json"
	"errors"
	"fmt"
	"reagent/config"
	"reagent/messenger/topics"
	"reagent/release"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/rs/zerolog/log"
)

var ContainerNameRegExp = `(.{3,4})_([0-9]*)_.*`

func BuildContainerName(stage Stage, appKey uint64, appName string) string {
	return strings.ToLower(fmt.Sprintf("%s_%d_%s", stage, appKey, appName))
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

func EnvironmentVarsToStringArray(environmentsMap map[string]interface{}) []string {
	stringArray := make([]string, 0)

	for key, entry := range environmentsMap {
		value := entry.(map[string]interface{})["value"]
		stringArray = append(stringArray, fmt.Sprintf("%s=%s", key, fmt.Sprint(value)))
	}

	return stringArray
}

func ParseExitCodeFromContainerStatus(status string) (int64, error) {
	statusString := regexp.MustCompile(`\((.*?)\)`).FindString(status)
	exitCodeString := strings.TrimRight(strings.TrimLeft(statusString, "("), ")")
	exitCodeInt, err := strconv.ParseInt(exitCodeString, 10, 64)
	if err != nil {
		return -1, err
	}

	return exitCodeInt, nil
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

func PrettyFormat(data interface{}) (string, error) {
	var p []byte
	//    var err := error
	p, err := json.MarshalIndent(data, "", "\t")
	if err != nil {
		return "", err
	}

	return string(p), nil
}
