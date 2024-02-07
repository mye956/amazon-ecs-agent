#!/bin/bash
              REGISTERED=$(curl -s http://localhost:51678/v1/metadata)
              AZ=$(curl -s http://169.254.169.254/latest/meta-data/placement/availability-zone)
              REGION=$(echo "$AZ" | sed 's/[a-z]$//')
              EC2_INSTANCE_ID=$(curl http://169.254.169.254/latest/meta-data/instance-id)
              ECS_STATUS=""
              DOCKER_STATUS=""
              CONTAINERD_STATUS=""
              DOCKER_CONTAINER_EXIT_CODE=""
              DOCKER_CONTAINER_ERROR=""
              DOCKER_CONTAINER_OOM=""
              AGENT_EXIT_CODE=""
              AGENT_ERROR=""
              AGENT_OOM=""
              ECS_CONFIG_FILE_ERROR="false"
              NVIDIA_DRIVER_AVAILABLE="false"
              DOCKER_STATE_ERROR="{{.State.Error}}"
              DOCKER_STATE_OOMKILLED="{{.State.OOMKilled}}"
              DOCKER_STATE_EXIT_CODE="{{.State.ExitCode}}"

              if [  -z "$REGISTERED" ]; then
                rm validation_output.log
                echo "Warning! Container instance is not registered to ECS. [Reported Instance ID: $EC2_INSTANCE_ID]" >> validation_output.log
                echo "----Summary----" >> validation_output.log
      
                ERROR_OUTPUT=$(cat /var/log/ecs/ecs-agent.log | grep "Unable to register as a container instance with ECS" | tail -1)
                if [[ ! -z "$ERROR_OUTPUT" ]]; then 
                  echo "Register Container Instance Output: $ERROR_OUTPUT" >> validation_output.log
                fi
                echo "Checking status of essential system services..." >> validation_output.log
    
                ECS_STATUS=$(systemctl is-active ecs)
                echo "* ECS Status: $ECS_STATUS" >> validation_output.log

                DOCKER_STATUS=$(systemctl is-active docker)
                echo "* Docker Status: $DOCKER_STATUS" >> validation_output.log

                CONTAINERD_STATUS=$(systemctl is-active containerd)
                echo "* Containerd Status: $CONTAINERD_STATUS" >> validation_output.log

                echo "Conducting Docker lifecycle test..." >> validation_output.log
                IMAGE_BUILD_OUTPUT=$(docker build -t aws-docker-test .)
                if [ $? -eq 0 ]; then
                  echo "* Docker Image Build Status: Success" >> validation_output.log
                  docker run --name aws-docker-test aws-docker-test
                  sleep 5
                  if [ "$(docker ps -aq -f status=exited -f name=aws-docker-test)" ]; then
                    echo "* Docker Container Test Run: Success" >> validation_output.log
                  else
                    echo "* Docker Container Test Run:: Failed" >> validation_output.log
                  fi
                  DOCKER_CONTAINER_EXIT_CODE=$(docker inspect aws-docker-test --format="$DOCKER_STATE_EXIT_CODE")
                  echo $DOCKER_CONTAINER_EXIT_CODE
                  if [[ $? -eq 0 && $DOCKER_CONTAINER_EXIT_CODE != "0" ]]; then
                    echo $DOCKER_CONTAINER_EXIT_CODE
                    DOCKER_CONTAINER_ERROR=$(docker inspect aws-docker-test --format="$DOCKER_STATE_ERROR")
                    DOCKER_CONTAINER_OOM=$(docker inspect aws-docker-test --format="$DOCKER_STATE_OOMKILLED")
                    echo "* Docker Container Exit Code: $DOCKER_CONTAINER_EXIT_CODE" >> validation_output.log
                    echo "* Docker Container Error: $DOCKER_CONTAINER_ERROR" >> validation_output.log
                    echo "* Docker Container OOM: $DOCKER_CONTAINER_OOM" >> validation_output.log
                  fi
                  docker rm aws-docker-test
                  docker image rm aws-docker-test
                  docker image rm public.ecr.aws/docker/library/busybox:uclibc
                  rm Dockerfile
                else
                echo "* Docker Image Build Status: Failed with $IMAGE_BUILD_OUTPUT" >> validation_output.log
                fi

                echo "Checking status of ECS Agent..." >> validation_output.log 
                AGENT_EXIT_CODE=$(docker inspect ecs-agent --format="$DOCKER_STATE_EXIT_CODE")
                AGENT_ERROR=$(docker inspect ecs-agent --format="$DOCKER_STATE_ERROR")
                AGENT_OOM=$(docker inspect ecs-agent --format="$DOCKER_STATE_OOMKILLED")
                echo "* Agent Container Exit Code: $AGENT_EXIT_CODE" >> validation_output.log
                echo "* Agent Container Error: $AGENT_ERROR" >> validation_output.log
                echo "* Agent Container OOM: $AGENT_OOM" >> validation_output.log

                echo "Checking if ECS endpoint is reachable..." >> validation_output.log
                ECS_ENDPOINT="ecs.$REGION.amazonaws.com"
                IS_ENDPOINT_REACHABLE=$(ping -c 1 "$ECS_ENDPOINT" | grep "1 packets transmitted, 1 received")

                if [[ -z "$IS_ENDPOINT_REACHABLE" ]]; then
                  echo "* ECS Endpoint Reachability: Unable to reach $ECS_ENDPOINT" >> validation_output.log
                else
                  echo "* ECS Endpoint Reachability: Able to reach $ECS_ENDPOINT" >> validation_output.log
                fi

                echo "Validating ECS Configuration file..." >> validation_output.log 
                while IFS= read -r line; do
                  if [[ "${line:0:1}" = "#"  || -z "$line" ]]; then
                    continue
                  fi
                  validate_line=$(echo ${line} | grep -E "^[A-Za-z0-9_].+=.+$")
                  if [[ ${validate_line} == "" ]]; then
                    ECS_CONFIG_FILE_ERROR="true"
                    echo "* Error in ECS configuration file wtih invalid contents: ${line}" >> validation_output.log
                  fi
                done < "/etc/ecs/ecs.config"
        
                echo "Checking Nvidia driver status" >> validation_output.log 
                NVIDIA_DRIVER=$(nvidia-smi -L)
                if [ $? -eq 0 ]; then
                    NVIDIA_DRIVER_AVAILABLE="true"
                  echo "* Nvidia Driver Status: $NVIDIA_DRIVER" >> validation_output.log
                fi

                echo "----Analysis----" >> validation_output.log

                if [[ $ECS_STATUS = "inactive" || $DOCKER_STATUS = "inactive" || $CONTAINERD_STATUS = "inactive" ]]; then 
                  echo "* One or more essential service is inactive. Please ensure that either ECS, Docker, and Containerd is up and running. To further debug, you can obtain the full logs via: journalctl -u <SERVICE_NAME>.service" >> validation_output.log
                fi

                if [[ $DOCKER_CONTAINER_OOM = "true" || $AGENT_OOM = "true" ]]; then 
                  echo "* Unable to start up docker containers. Please ensure that there's enough resouces allocated within the instance. Refer to the following link for more information regarding OOM: https://docs.docker.com/config/containers/resource_constraints/" >> validation_output.log
                fi

                 if [[ $DOCKER_CONTAINER_EXIT_CODE != "0" ]]; then
                  echo "* Error while running test container $DOCKER_EXIT_CODE. Please refer to the following link regarding docker diagnostics: https://docs.aws.amazon.com/AmazonECS/latest/developerguide/docker-diags.html" >> validation_output.log
                fi

                if [[ $AGENT_EXIT_CODE != "0" ]]; then
                  echo "* Agent was unable to start up properly. Please refer to the following doc to obtain agent logs: https://docs.aws.amazon.com/AmazonECS/latest/developerguide/ecs-logs-collector.html." >> validation_output.log
                fi

                if [[ -z "$IS_ENDPOINT_REACHABLE" ]]; then
                  echo "* Unable to reach ECS Endpoint. Please ensure that your networking configuration is properly set up. https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-networking.html" >> validation_output.log
                fi

                if [[ $ECS_CONFIG_FILE_ERROR = "true" ]]; then
                  echo "* Error found in ECS configuration file. Please ensure that the config file is properly formatted. Refer to the following link regarding agent configuration options: https://docs.aws.amazon.com/AmazonECS/latest/developerguide/ecs-agent-config.html" >> validation_output.log
                fi

                if [[ $NVIDIA_DRIVER_AVAILABLE = "false" ]]; then
                    echo "* Unable to check Nvidia Driver Status. Disregard if the AMI is not GPU-supported, otherwise please ensure that the nvidia-smi library is installed." >> validation_output.log
                fi
                cat validation_output.log
                exit 8
              else
                echo "Container Instance is registered to an ECS Cluster" >> validation_output.log
                cat validation_output.log
                exit 0
              fi