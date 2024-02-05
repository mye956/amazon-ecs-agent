#!/bin/bash

REGION=""
TERMINATE_ENABLED="false"
WAIT_TIME="300"
ASG_NAME=""

PROGNAME="${0##*/}"

usage() {
    cat <<-EOF
Usage:
  $0

Options:
	--region  (Required) Region where the resources are to be created.
	--asg-name   (Required) Name(s) of the Autoscaling Group to track for unregistered/orphan instances. Multiple ASGs can be specified by using "," as the delimiter. (e.g. Name1,Name2,...)
	--wait-time  (Optional) The amount of duration in seconds of when to check for orphan instances within an Autoscaling Group.
	--instance-cleanup        (Optional) Whether to clean up the instance if it's been detected as an unregistered/orphan instance.

Example:
  $0 --region us-east-1 --asg-name SomeName --wait-time 300 --instance-cleanup
EOF
}

POSITIONAL_ARGS=()

parse_args() {
    
    while :; do
        case $1 in
            --region)
                REGION=$2
                echo $REGION
                shift 2
                ;;
            --asg-name)
                ASG_NAME=$2
                shift 2
                ;;
            --wait-time)
                WAIT_TIME=$2
                shift 2
                ;;
            --instance-cleanup)
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
        exit 2
    fi

    if [[ -z "${ASG_NAME}" ]]; then
        echo "ERROR: Autoscaling group name must not be empty"
        exit 2
    fi
}

main() {

    TMPDIR=$(mktemp -d)
    echo "Created temporary directory: $TMPDIR"
    cd ${TMPDIR}

    

    parse_args "$@"
    validate_args

    curl https://raw.githubusercontent.com/mye956/amazon-ecs-agent/orphan-instance/orphan-instance/orphan-instance-stack.yml -o orphan-instance-stack.yml
    curl -L https://github.com/mye956/amazon-ecs-agent/raw/orphan-instance/orphan-instance/myFunction.zip -o myFunction.zip

    ls -l ./myFunction.zip

    bucket_name="orphan-instance-$REGION"
    bucket_exist=$(aws s3api list-buckets --query "Buckets[].Name" | grep "$bucket_name")
    
    if [[ -z "${bucket_exist}" ]]; then
        echo "Creating bucket"
        aws s3api create-bucket --bucket $bucket_name --region $REGION --create-bucket-configuration LocationConstraint=$REGION
    fi
    aws s3api wait bucket-exists --bucket $bucket_name
    echo "Orphan instance S3 bucket exists"

    aws s3 cp ./orphan-instance-stack.yml s3://$bucket_name/orphan-instance-stack.yml --region $REGION
    aws s3 cp ./myFunction.zip s3://$bucket_name/myFunction.zip --region $REGION

    aws cloudformation create-stack --stack-name orphan-instance --template-body file://orphan-instance-stack.yml --region $REGION --parameters ParameterKey=AutoScalingGroupName,ParameterValue=$ASG_NAME ParameterKey=WaitTimer,ParameterValue=$WAIT_TIME ParameterKey=TerminateEnabled,ParameterValue=$TERMINATE_ENABLED ParameterKey=S3BucketName,ParameterValue=$bucket_name --capabilities CAPABILITY_NAMED_IAM

    echo "Deleting temporary directory $TMPDIR..."
    rm -rf $TMPDIR
}

main "$@"