package common

import (
	"encoding/json"
	"fmt"
	"reagent/config"
	"strings"

	"github.com/rs/zerolog/log"
)

func BuildContainerName(stage Stage, appKey uint64, appName string) string {
	return strings.ToLower(fmt.Sprintf("%s_%d_%s", stage, appKey, appName))
}

func BuildImageName(stage Stage, arch string, appKey uint64, appName string) string {
	return strings.ToLower(fmt.Sprintf("%s_%s_%d_%s", stage, arch, appKey, appName))
}

func BuildRegistryImageName(registryURL string, mainRepositoryName string, imageName string) string {
	return strings.ToLower(fmt.Sprintf("%s%s%s", registryURL, mainRepositoryName, imageName))
}

const topicPrefix = "re.mgmt"

func BuildExternalApiTopic(serialNumber string, topic string) string {
	return fmt.Sprintf("%s.%s.%s", topicPrefix, serialNumber, topic)
}

func BuildDockerBuildID(appKey uint64, appName string) string {
	return fmt.Sprintf("build_%d_%s", appKey, appName)
}

func EnvironmentVarsToStringArray(environmentsMap map[string]interface{}) []string {
	stringArray := make([]string, 0)

	for key, entry := range environmentsMap {
		value := entry.(map[string]interface{})["value"]
		stringArray = append(stringArray, fmt.Sprintf("%s=%s", key, fmt.Sprint(value)))
	}

	return stringArray
}

func (tp *TransitionPayload) initContainerData(appKey uint64, appName string, config *config.Config) {
	publishContainer := BuildContainerName("pub", appKey, appName)
	devContainerName := BuildContainerName(DEV, appKey, appName)
	prodContainerName := BuildContainerName(PROD, appKey, appName)

	devImageName := BuildImageName(DEV, config.ReswarmConfig.Architecture, appKey, appName)
	devRegImageName := BuildRegistryImageName(config.ReswarmConfig.DockerRegistryURL, config.ReswarmConfig.DockerMainRepository, devImageName)

	prodImageName := BuildImageName(PROD, config.ReswarmConfig.Architecture, appKey, appName)
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

func PrettyFormat(data interface{}) (string, error) {
	var p []byte
	//    var err := error
	p, err := json.MarshalIndent(data, "", "\t")
	if err != nil {
		fmt.Println(err)
		return "", err
	}

	return string(p), nil
}
