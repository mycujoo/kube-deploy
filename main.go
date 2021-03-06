package main

import (
	"bufio"
	"io/ioutil"
	"log"
	"strconv"
	"strings"

	// "flag"
	"fmt"
	"os"

	// "os/user"
	"github.com/mycujoo/kube-deploy/build"
	"github.com/mycujoo/kube-deploy/cli"
	"github.com/mycujoo/kube-deploy/config"
	"github.com/simonleung8/flags"
)

// var userConfig userConfigMap
var repoConfig config.RepoConfigMap
var reader *bufio.Reader
var osstdout *os.File

func main() {
	parseFlags()
	pwd, _ := os.Getwd()
	// user, _ := user.Current()
	// userHome := user.HomeDir
	reader = bufio.NewReader(os.Stdin)

	osstdout = os.Stdout
	if runFlags.Bool("quiet") {
		os.Stdout = nil
	}
	// TODO: for some reason, on a linux machine, if any command other than 'curl' is executed first, all
	//		 subcommands fail - but sometimes, the first-run after 'go build' works. Who knows...
	if exitCode := cli.GetCommandExitCode("curl", "-s --connect-timeout 3 https://google.com"); exitCode != 0 {
		log.Fatal("=> Uh oh, looks like you're not connected to the internet (or maybe it's just too slow).")
	}

	if !runFlags.Bool("test-only") {
		fmt.Println("=> First, I'm going to read the repo configuration file.")
		repoConfig = config.InitRepoConfig(fmt.Sprintf("%s/deploy.yaml", pwd))
		fmt.Printf(`=> I found the following data:
	Registry root: %s
	Repository name: %s
	Current branch: %s
	HEAD hash: %s
	Namespace: %s

=> That means we're dealing with the image tag:
	%s
`, repoConfig.DockerRepository.RegistryRoot, repoConfig.Application.Name, repoConfig.GitBranch, repoConfig.GitSHA, repoConfig.EnvVarsMap.GetNameSpace(), repoConfig.ImageFullPath)
	}

	// args has to have at least length 2, since the first element is the executable name
	if len(args) >= 2 {
		fmt.Printf("\n=> You've chosen the action '%s'. Proceeding...\n----------\n\n", args[1])

		switch c := args[1]; c {

		case "name":
			fmt.Fprintln(osstdout, repoConfig.ImageFullPath)
		case "environment":
			fmt.Fprintln(osstdout, repoConfig.EnvVarsMap.GetNameSpace())
		case "cluster":
			fmt.Fprintln(osstdout, repoConfig.ClusterName)
		case "release":
			fmt.Fprintln(osstdout, repoConfig.ReleaseName)

		case "build":
			build.MakeAndPushBuild(
				runFlags.Bool("force-push-image"),
				runFlags.Bool("override-dirty-workdir"),
				runFlags.Bool("keep-test-container"),
				repoConfig,
			)
		case "make":
			build.MakeAndPushBuild(
				runFlags.Bool("force-push-image"),
				runFlags.Bool("override-dirty-workdir"),
				runFlags.Bool("keep-test-container"),
				repoConfig,
			)
		case "test":
			build.MakeAndTestBuild(
				runFlags.Bool("override-dirty-workdir"),
				runFlags.Bool("keep-test-container"),
				repoConfig,
			)
		case "testonly":
			build.RunBuildTests(runFlags.Bool("keep-test-container"), repoConfig)

		case "start-rollout":
			kubeStartRollout()
		case "scale":
			replicas, _ := strconv.ParseInt(args[2], 0, 32)
			kubeScaleDeployment(int32(replicas))
		case "rollback":
			kubeInstantRollback()
		case "rolling-restart":
			kubeRollingRestart()
		case "template-only":
			fmt.Println("The files can be found at: ")
			fmt.Fprint(osstdout, strings.Join(kubeMakeTemplates(), "\n"))

		case "remove":
			kubeRemove()

		case "active-deployments":
			kubeListDeployments()
		case "list-tags":
			build.DockerListTags(repoConfig.ImageName)

		case "status":
			if status := cli.IsLocked(repoConfig.Application.Name); status == false {
				fmt.Print("=> No rollout in progress for this repo and branch.\n\n")
			}

		case "lock":
			cli.WriteLockFile(repoConfig.Application.Name, "manually blocked rollouts for "+repoConfig.Application.Name)
		case "unlock":
			cli.DeleteLockFile(repoConfig.Application.Name)
		case "lock-all":
			cli.WriteLockFile("all", "manually blocked all rollouts")
		case "unlock-all":
			cli.DeleteLockFile("all")
		default:
			{
				fmt.Println("=> Uh oh - that command isn't recongised. Please enter a valid command. Do you need some help?")
				fmt.Print("=> Press 'y' to show the help menu, anything else to exit.\n>>>  ")
				pleaseHelpMe, _ := reader.ReadString('\n')
				if pleaseHelpMe != "y\n" && pleaseHelpMe != "Y\n" {
					log.Fatal("Better luck next time.")
				}
				showHelp()
			}
		}
	} else {
		log.Fatal("You'll need to add a command.")
	}
}

func askToProceed(promptMessage string) bool {
	fmt.Printf("=> %s\n=> Press 'y' to proceed, anything else to exit.\n>>> ", promptMessage)
	if proceed, _ := reader.ReadString('\n'); proceed != "y\n" && proceed != "Y\n" {
		return false
	}
	return true
}

func showHelp() {
	helpData, err := ioutil.ReadFile("README.md")
	// TODO: make this part of the application bundle, since right now it will print the README of whatever project you're trying to deploy :|
	if err != nil {
		fmt.Println("=> Oh no, we couldn't even read the help file!")
		panic(err)
	}
	fmt.Print(string(helpData))
}

var args []string
var runFlags flags.FlagContext

func parseFlags() {

	runFlags = flags.New()
	runFlags.NewBoolFlag("debug", "", "Print extra-fun information.")
	runFlags.NewBoolFlag("override-dirty-workdir", "", "Forces a build even if the git working directory is dirty (only needed for 'production' and 'master' branches).")
	runFlags.NewBoolFlag("force", "", "Unwisely bypasses the sanity checks, which you really need. Even you.")
	runFlags.NewBoolFlag("force-push-image", "", "Automatically push the built Docker image if the tests pass (useful for CI/CD).")
	runFlags.NewBoolFlag("keep-test-container", "", "Don't clean up (docker rm) the test containers (Default false).")
	runFlags.NewBoolFlag("no-canary", "", "Bypass the canary release points (useful for CI/CD).")
	runFlags.NewBoolFlag("no-build", "", "Skip build during rollout")
	runFlags.NewBoolFlag("test-only", "", "Skips the run configuration and only tests that the binary can start.")
	runFlags.NewBoolFlag("quiet", "q", "Silences as much output as possible.")
	runFlags.NewBoolFlag("keep-kubernetes-template-files", "", "Leaves the templated-out kubernetes files under the directory '.kubedeploy-temp'.")
	if err := runFlags.Parse(os.Args...); err != nil {
		log.Println("\n=> Oh no, I don't know what to do with those command line flags. Sorry...")
		log.Fatal(runFlags.ShowUsage(4))
	}
	args = runFlags.Args()
}
