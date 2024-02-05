#!/bin/bash

REGION=""
ASG_NAME=""

PROGNAME="${0##*/}"

usage() {
    cat <<-EOF
Usage:
  $0

Options:
	--region  (Required) Region where the resources are to be created.
	--asg-name   (Required) Name of the Autoscaling Group to track for unregistered/orphan instances.

Example:
  $0 --region us-east-1 --asg-name SomeName
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
    parse_args "$@"
    validate_args

    for ID in $(aws autoscaling describe-auto-scaling-instances --region $REGION --query AutoScalingInstances[?AutoScalingGroupName==\'$ASG_NAME\'].InstanceId --output text);
    do
        aws lambda invoke --region $REGION --cli-binary-format raw-in-base64-out  --invocation-type Event --payload "{ \"detail\": { \"EC2InstanceId\": \"$ID\" } }" --function-name ECSOrphanInstanceLambda --cli-binary-format raw-in-base64-out response.json
        cat response.json
    done

}
main "$@"
