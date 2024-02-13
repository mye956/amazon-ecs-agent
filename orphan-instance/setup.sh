#!/bin/bash

TERMINATE_ENABLED="false"
WAIT_TIME="300"
ASG_NAME=""

usage() {
    cat <<-EOF
Usage:
  $0

Options:
	--asg-name              (Required) Name(s) of the Autoscaling Group to track for unregistered/orphan instances. Multiple ASGs can be specified by using "," as the delimiter. (e.g. Name1,Name2,...)
	--wait-time             (Optional) The amount of duration in seconds of when to check for orphan instances within an Autoscaling Group.
	--enable-cleanup        (Optional) Whether to clean up the instance if it's been detected as an unregistered/orphan instance.

Example:
  AWS_REGION=us-east-1 $0 --asg-name SomeName --wait-time 300 --enable-cleanup
EOF
}

parse_args() {
    
    while :; do
        case $1 in
            --asg-name)
                ASG_NAME=\"$2\"
                shift 2
                ;;
            --wait-time)
                WAIT_TIME=$2
                shift 2
                ;;
            --enable-cleanup)
                TERMINATE_ENABLED="true"
                shift
                ;;
            --help)
                usage
                exit 0
                ;;
            --)
                shift
                break
                ;;
            --*)
                echo "ERROR: unsupported flag $1">&2
                exit 1
                ;;
            *)
                break ;;
        esac
    done
}

validate_args(){
    if [[ -z "${ASG_NAME}" ]]; then
        echo "ERROR: Autoscaling group name must not be empty"
        exit 1
    fi
}

main() {
    echo "Setting up resources for ECS Orphan Instance Diagnostic Checker..."

    parse_args "$@"
    validate_args

    TMPDIR=$(mktemp -d)
    trap "rm -rf $TMPDIR" EXIT
    cd "${TMPDIR}" || exit 1

    echo "Downloading Orphan Instance Cloudformation template..."
    # TODO: Update this to the correct link
    # Note: The correct location of of this file will be updated once the public Github repository has been created
    curl https://raw.githubusercontent.com/mye956/amazon-ecs-agent/orphan-instance/orphan-instance/orphan-instance-stack.yml -o orphan-instance-stack.yml

    aws cloudformation describe-stacks --stack-name ecs-orphan-instance-detector --region $AWS_REGION > /dev/null 2>&1
    if [ $? -eq 0  ]; then
        echo "ECS Orphan Instance Detector Cloudformation Stack already exists. Updating existing stack..."
        aws cloudformation update-stack --stack-name ecs-orphan-instance-detector --template-body file://orphan-instance-stack.yml --region $AWS_REGION \
         --parameters ParameterKey=AutoScalingGroupName,ParameterValue=$ASG_NAME ParameterKey=WaitTimer,ParameterValue=$WAIT_TIME \
         ParameterKey=TerminateEnabled,ParameterValue=$TERMINATE_ENABLED --capabilities CAPABILITY_NAMED_IAM
        if [ $? -ne 0 ]; then
            echo "ERROR: Unable to update stack"
            exit 1
        fi
        aws cloudformation wait stack-update-complete --stack-name ecs-orphan-instance-detector --region $AWS_REGION
    else
        echo "Creating Cloudformation stack..."
        aws cloudformation create-stack --stack-name ecs-orphan-instance-detector --template-body file://orphan-instance-stack.yml --region $AWS_REGION \
        --parameters ParameterKey=AutoScalingGroupName,ParameterValue=$ASG_NAME ParameterKey=WaitTimer,ParameterValue=$WAIT_TIME \
        ParameterKey=TerminateEnabled,ParameterValue=$TERMINATE_ENABLED --capabilities CAPABILITY_NAMED_IAM
        if [ $? -ne 0 ]; then
            echo "ERROR: Unable to create stack"
            exit 1
        fi
        aws cloudformation wait stack-create-complete --stack-name ecs-orphan-instance-detector --region $AWS_REGION
    fi
    
    echo "Cloudfromation stack is ready."
    echo "ECS Orphan Instance Dianostic Checker setup finished."
}

main "$@"