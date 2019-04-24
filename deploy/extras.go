package deploy

import (
	helpers "../blockchains/helpers"
	db "../db"
	ssh "../ssh"
	state "../state"
	testnet "../testnet"
	util "../util"
	"encoding/base64"
	"fmt"
	"log"
	"sync"
)

func distributeNibbler(tn *testnet.TestNet) {
	buildState.Async(
		func() {
			nibbler, err := util.HttpRequest("GET", "https://storage.googleapis.com/genesis-public/nibbler/master/bin/linux/amd64/nibbler", "")
			if err != nil {
				log.Println(err)
			}
			err = buildState.Write("nibbler", string(nibbler))
			if err != nil {
				log.Println(err)
			}
			err = helpers.CopyToAllNodes(servers, clients, buildState,
				"nibbler", "/usr/local/bin/nibbler")
			if err != nil {
				log.Println(err)
			}
			err = helpers.AllNodeExecCon(servers, buildState, func(serverNum int, localNodeNum int, absoluteNodeNum int) error {
				_, err := clients[serverNum].DockerExec(localNodeNum, "chmod +x /usr/local/bin/nibbler")
				return err
			})
			if err != nil {
				log.Println(err)
			}
		})
}

func handleDockerBuildRequest(blockchain string, prebuild map[string]interface{},
	clients []*ssh.Client, buildState *state.BuildState) error {

	_, hasDockerfile := prebuild["dockerfile"] //Must be base64
	if !hasDockerfile {
		return fmt.Errorf("Cannot build without being given a dockerfile")
	}

	dockerfile, err := base64.StdEncoding.DecodeString(prebuild["dockerfile"].(string))
	if err != nil {
		log.Println(err)
		return err
	}
	err = buildState.Write("Dockerfile", string(dockerfile))
	if err != nil {
		log.Println(err)
	}

	err = helpers.CopyAllToServers(clients, buildState, "Dockerfile", "/home/appo/Dockerfile")
	if err != nil {
		log.Println(err)
		return err
	}

	tag, err := util.GetUUIDString()
	if err != nil {
		log.Println(err)
		return err
	}
	buildState.SetBuildStage("Building your custom image")
	imageName := fmt.Sprintf("%s:%s", blockchain, tag)
	wg := sync.WaitGroup{}
	for _, client := range clients {
		wg.Add(1)
		go func(client *ssh.Client) {
			defer wg.Done()

			_, err := client.Run(fmt.Sprintf("docker build /home/appo/ -t %s", imageName))
			buildState.Defer(func() { client.Run(fmt.Sprintf("docker rmi %s", imageName)) })
			if err != nil {
				log.Println(err)
				buildState.ReportError(err)
				return
			}

		}(client)
	}
	wg.Wait()
	if !buildState.ErrorFree() {
		return buildState.GetError()
	}
	return nil
}

func handlePreBuildExtras(tn *testnet.TestNet) error {
	if buildConf.Extras == nil {
		return nil //Nothing to do
	}
	_, exists := buildConf.Extras["prebuild"]
	if !exists {
		return nil //Nothing to do
	}
	prebuild, ok := buildConf.Extras["prebuild"].(map[string]interface{})
	if !ok || prebuild == nil {
		return nil //Nothing to do
	}
	//

	dockerBuild, ok := prebuild["build"] //bool to see if a manual build was requested.
	if ok && dockerBuild.(bool) {
		err := handleDockerBuildRequest(buildConf.Blockchain, prebuild, clients, buildState)
		if err != nil {
			log.Println(err)
			return err
		}
	}

	dockerPull, ok := prebuild["pull"]
	if ok && dockerPull.(bool) {
		wg := sync.WaitGroup{}
		for _, image := range buildConf.Images {
			wg.Add(1)
			go func(image string) {
				defer wg.Done()
				err := DockerPull(clients, image)
				if err != nil {
					log.Println(err)
					buildState.ReportError(err)
					return
				}
			}(image)
		}
		wg.Wait()
	}

	return buildState.GetError()
}
