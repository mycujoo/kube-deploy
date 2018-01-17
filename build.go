package main

import (
	"fmt"
	"os"
	"strings"
	"time"
	//	"os/exec"
)

const testCommandImage = "mycujoo/gcloud-docker"

func makeAndPushBuild() {
	makeAndTestBuild()
	if runFlags.Bool("force-push-image") {
		forcePushDockerImage()
	} else {
		askPushDockerImage()
	}
}
func makeAndTestBuild() {
	if !dockerAmLoggedIn() {
		fmt.Println("=> Uh oh, you're not logged into the configured docker remote for this repo. You won't be able to push!")
		os.Exit(1)
	}
	makeBuild()
	runBuildTests()
	tagDockerImage()
}

func checkWorkingDirectory() bool {
	// Returns 'true' for clean, 'false' for dirty
	if runFlags.Bool("override-dirty-workdir") {
		fmt.Println("=> Respecting your wishes to override the dirty working directory and build anyway.")
		return true
	}

	dirtyWorkingDirectory := []int{
		getCommandExitCode("git", "diff-index --quiet HEAD --"),       // checks for modified files
		getCommandExitCode("test", "-z \"$(git ls-files --others)\"")} // checks for untracked files
	for _, code := range dirtyWorkingDirectory {
		if code != 0 {
			return false
		}
	}
	return true
}

func makeBuild() {
	// Builds the docker image and tags it with the image short-name (ie. without the registry path)
	if repoConfig.ClusterName == "production" {
		if !checkWorkingDirectory() {
			fmt.Println("=> Oh no! You have uncommited changes in the working tree. Please commit or stash before deploying to production.")
			fmt.Println("=> If you're really, really sure, you can override this warning with the '--override-dirty-workdir' flag.")
			os.Exit(1)
		}
	}

	fmt.Println("=> Okay, let's start the build process!")
	fmt.Printf("=> First, let's build the image with tag: %s\n\n", repoConfig.ImageName)
	time.Sleep(1 * time.Second)

	// Run docker build
	if exitCode := streamAndGetCommandExitCode(
		"docker",
		fmt.Sprintf("build -t %s %s", repoConfig.ImageName, repoConfig.PWD),
	); exitCode != 0 {
		os.Exit(1)
	}
}

func runBuildTests() {
	// Start container and run tests
	tests := repoConfig.Tests
	for _, testSet := range tests {
		fmt.Printf("\n\n=> Setting up test set: %s\n", testSet.Name)

		// Start the test container
		var (
			containerName string
			exitCode      int
		)
		if testSet.Type != "host-only" { // 'host-only' skips running the test docker container (for env setup)
			fmt.Printf("=> Starting docker image: %s\n", repoConfig.ImageName)

			var dockerRunCommand string
			if testSet.DockerArgs != "" {
				dockerRunCommand = fmt.Sprintf("%s %s", testSet.DockerArgs, repoConfig.ImageName)
			} else {
				dockerRunCommand = repoConfig.ImageName
			}
			if testSet.DockerCommand != "" {
				dockerRunCommand = dockerRunCommand + " " + testSet.DockerCommand
			}

			containerName, exitCode = streamAndGetCommandOutputAndExitCode("docker",
				strings.Join([]string{"run", dockerRunCommand}, " "))
			if runFlags.Bool("debug") {
				fmt.Println(containerName)
			}
			if exitCode != 0 {
				teardownTest(containerName, true)
			}
		}

		// Wait two seconds for it to come alive
		time.Sleep(2 * time.Second)

		// Run all tests
		for _, testCommand := range testSet.Commands {
			// Wait two seconds for it to come alive
			time.Sleep(2 * time.Second)
			fmt.Printf("=> Executing test command: %s\n", testCommand)
			// commandSplit := strings.SplitN(testCommand, " ", 2)
			// Run the test command
			switch t := testSet.Type; t {
			case "on-host", "host-only":
				commandSplit := strings.SplitN(testCommand, " ", 2)
				if exitCode := streamAndGetCommandExitCode(commandSplit[0], commandSplit[1]); exitCode != 0 {
					teardownTest(containerName, true)
					break
				}
			case "in-test-container":
				if exitCode := streamAndGetCommandExitCode("docker", fmt.Sprintf("exec %s %s", containerName, testCommand)); exitCode != 0 {
					teardownTest(containerName, true)
					break
				}
			case "in-external-container":
				if exitCode := streamAndGetCommandExitCode("docker", fmt.Sprintf("run --rm --network container:%s %s %s", containerName, testCommandImage, testCommand)); exitCode != 0 {
					teardownTest(containerName, true)
					break
				}
			default:
				fmt.Printf("=> Since you didn't specify where to run test %s, I'll run it in a test container (attached to the same network).\n", testCommand)
				if exitCode := streamAndGetCommandExitCode("docker", fmt.Sprintf("run --rm --network container:%s %s %s", containerName, testCommandImage, testCommand)); exitCode != 0 {
					teardownTest(containerName, true)
				}
			}
		}
		teardownTest(containerName, false)
	}
}

func teardownTest(containerName string, exit bool) {
	if containerName != "" {
		fmt.Println("=> Stopping test container.")
		getCommandOutput("docker", fmt.Sprintf("stop %s", containerName))
		if runFlags.Bool("keep-test-container") {
			fmt.Println("=> Leaving the test container without deleting, like you asked.\n")
		} else {
			fmt.Println("=> Removing test container.")
			getCommandOutput("docker", fmt.Sprintf("rm %s", containerName))
		}
	}
	if exit {
		os.Exit(1)
	}
}

func tagDockerImage() {
	fmt.Printf("=> Tagging the image short name %s with the image full path:\n\t%s.\n\n", repoConfig.ImageName, repoConfig.ImageFullPath)
	getCommandOutput("docker", fmt.Sprintf("tag %s %s", repoConfig.ImageName, repoConfig.ImageFullPath))
}

func askPushDockerImage() {
	fmt.Print("=> Yay, all the tests passed! Would you like to push this to the remote now?\n=> Press 'y' to push, anything else to exit.\n>>> ") // TODO - make this pluggable
	confirm, _ := reader.ReadString('\n')
	if confirm != "y\n" && confirm != "Y" {
		fmt.Println("=> Thanks for building, Bob!")
		os.Exit(0)
	} else {
		streamAndGetCommandOutput("docker", fmt.Sprintf("push %s", repoConfig.ImageFullPath))
	}
}

func forcePushDockerImage() {
	streamAndGetCommandOutput("docker", fmt.Sprintf("push %s", repoConfig.ImageFullPath))
}
