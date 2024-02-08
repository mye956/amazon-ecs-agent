#!/bin/bash

REGION=""
TERMINATE_ENABLED="false"
WAIT_TIME="300"
ASG_NAME=""

usage() {
    cat <<-EOF
Usage:
  $0

Options:
	--region  (Required) Region where the resources are to be created.
	--asg-name   (Required) Name(s) of the Autoscaling Group to track for unregistered/orphan instances. Multiple ASGs can be specified by using "," as the delimiter. (e.g. Name1,Name2,...)
	--wait-time  (Optional) The amount of duration in seconds of when to check for orphan instances within an Autoscaling Group.
	--enable-cleanup        (Optional) Whether to clean up the instance if it's been detected as an unregistered/orphan instance.

Example:
  $0 --region us-east-1 --asg-name SomeName --wait-time 300 --enable-cleanup
EOF
}

parse_args() {
    
    while :; do
        case $1 in
            --region)
                REGION=$2
                shift 2
                ;;
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
    if [[ -z "${REGION}" ]]; then
        echo "ERROR: Region must not be empty"
        exit 1
    fi

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
    cd "${TMPDIR}" || exit 1

    echo "Downloading Orphan Instance Cloudformation template..."
    # TODO: Update this to the correct link
    # Note: The correct location of of this file will be updated once the public Github repository has been created
    curl https://raw.githubusercontent.com/mye956/amazon-ecs-agent/orphan-instance/orphan-instance/orphan-instance-stack.yml -o orphan-instance-stack.yml
    echo "Downloading Lambda function handler..."
    # TODO: Update this to the correct link
    # Note: The correct location of of this file will be updated once the public Github repository has been created
    # curl -L https://github.com/mye956/amazon-ecs-agent/raw/orphan-instance/orphan-instance/lambdaFunction.zip -o lambdaFunction.zip

    # bucket_name="orphan-instance-$REGION"
    # bucket_exist=$(aws s3api list-buckets --query "Buckets[].Name" | grep "$bucket_name")
    # if [[ -z "${bucket_exist}" ]]; then
    #     aws s3api create-bucket --bucket $bucket_name --region $REGION --create-bucket-configuration LocationConstraint=$REGION
    # fi
    # aws s3api wait bucket-exists --bucket $bucket_name
    # echo "Orphan Instance S3 bucket created."

    # echo "Copying over lambda function handler ZIP file..."
    # aws s3 cp ./lambdaFunction.zip s3://$bucket_name/lambdaFunction.zip --region $REGION

    echo "Creating Cloudformation stack..."
    aws cloudformation create-stack --stack-name orphan-instance --template-body file://orphan-instance-stack.yml --region $REGION --parameters ParameterKey=AutoScalingGroupName,ParameterValue=$ASG_NAME ParameterKey=WaitTimer,ParameterValue=$WAIT_TIME ParameterKey=TerminateEnabled,ParameterValue=$TERMINATE_ENABLED ParameterKey=S3BucketName,ParameterValue=$bucket_name --capabilities CAPABILITY_NAMED_IAM
    aws cloudformation wait stack-create-complete --stack-name orphan-instance --region $REGION
    echo "Cloudfromation stack has been created."

    rm -rf "$TMPDIR"
    echo "ECS Orphan Instance Dianostic Checker setup finished."
}

main "$@"