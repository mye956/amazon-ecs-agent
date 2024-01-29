package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ssm"
	log "github.com/cihub/seelog"
)

type MyEvent struct {
	Version string `json:"version"`
	Id      string `json:"id"`
	Detail  Detail `json:"detail"`
}

type Detail struct {
	InstanceId string `json:"EC2InstanceId"`
	StatusCode string `json:"StatusCode"`
}

// type Detail struct {
// 	InstanceId string `json:"instance-id"`
// }

func HandleRequest(ctx context.Context, event *MyEvent) (*string, error) {
	var exitCode int64
	if event == nil {
		return nil, fmt.Errorf("received nil event")
	}
	data, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	log.Infof("Event received: %s", string(data))

	documentName := "test-orphan-stack-ECSOrphanInstanceSSMDocument-OLO04TqElqbe"

	session, err := session.NewSession()
	if err != nil {
		return nil, err
	}

	svc := ec2.New(session)

	// Waiting for instance to be in running state
	for {
		time.Sleep(1 * time.Second)
		describeInput := &ec2.DescribeInstanceStatusInput{
			InstanceIds: aws.StringSlice([]string{event.Detail.InstanceId}),
		}
		describeOutput, err := svc.DescribeInstanceStatus(describeInput)
		if err != nil {
			return nil, err
		}
		// Breaking out of the loop since the instance has reached running state
		if *describeOutput.InstanceStatuses[0].InstanceState.Name == "running" {
			log.Infof("Instance %s has reached running state", event.Detail.InstanceId)
			break
		}
	}

	ssmClient := ssm.New(session)
	ssmInput := &ssm.SendCommandInput{
		DocumentName:    aws.String(documentName),
		DocumentVersion: aws.String("$LATEST"),
		CloudWatchOutputConfig: &ssm.CloudWatchOutputConfig{
			CloudWatchOutputEnabled: aws.Bool(true),
			CloudWatchLogGroupName:  aws.String("orphan-instance-log-group"),
		},
		Targets: []*ssm.Target{
			{
				Key:    aws.String("InstanceIds"),
				Values: aws.StringSlice([]string{event.Detail.InstanceId}),
			},
		},
	}

	// Sleeping to give ECS time to start up and attempt to register
	sleepTime, err := strconv.Atoi(os.Getenv("WAIT_TIME"))
	if err != nil {
		return nil, err
	}
	log.Infof("Sleeping for %d seconds to give ECS time to start up.", sleepTime)
	time.Sleep(time.Duration(sleepTime) * time.Second)

	// Invoking SSM RunCommand
	ssmOutput, err := ssmClient.SendCommand(ssmInput)
	if err != nil {
		return nil, err
	}
	commandId := *ssmOutput.Command.CommandId
	message := fmt.Sprintf("Executed SSM document wtih command ID: %s", commandId)
	log.Infof("Command ID: %s", commandId)

	for {
		time.Sleep(1 * time.Second)
		getCommandInput := &ssm.GetCommandInvocationInput{
			CommandId:  ssmOutput.Command.CommandId,
			InstanceId: aws.String(event.Detail.InstanceId),
		}

		getCommandOutput, err := ssmClient.GetCommandInvocation(getCommandInput)
		if err != nil {
			continue
		}

		if *getCommandOutput.Status != "InProgress" {
			exitCode = *getCommandOutput.ResponseCode
			break
		}
	}
	log.Info("SSM script has finished executing.")

	shouldTerminate := os.Getenv("TERMINATE_INSTANCE")
	if exitCode == 8 && shouldTerminate == "true" {
		log.Infof("RegisterContainerInstance failure detected. Terminating instance...")

		ec2Input := &ec2.TerminateInstancesInput{
			InstanceIds: []*string{
				aws.String(event.Detail.InstanceId),
			},
		}
		_, err := svc.TerminateInstances(ec2Input)
		if err != nil {
			return nil, err
		}

		for {
			time.Sleep(1 * time.Second)
			describeInput := &ec2.DescribeInstanceStatusInput{
				InstanceIds: aws.StringSlice([]string{event.Detail.InstanceId}),
			}
			describeOutput, err := svc.DescribeInstanceStatus(describeInput)
			if err != nil {
				return nil, err
			}
			// Breaking out of the loop since the instance has successfully terminated
			if len(describeOutput.InstanceStatuses) == 0 || *describeOutput.InstanceStatuses[0].InstanceState.Name == "terminated" {
				break
			}
		}
		log.Infof("Instance with ID: %s, has successfully been terminated", event.Detail.InstanceId)
	}
	return &message, nil
}

func main() {
	lambda.Start(HandleRequest)
}
