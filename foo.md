# ECSAgentOrphanInstanceCheck

### Description
This package contains all of the source code that's neccessary for creating and setting up the orphan instance diagnostic checker. This will be used for customers that want to have a mechanism to detect whether or not an instance within their autoscaling group/capacity providers was unable to register to ECS.

---

### How does it work?
An SSM document will be created that contains the acutal dianostic check script and is what will be executed on newly launched EC2 instances. This document will be triggered by a lambda function that looks for any launch events for a sepcific autoscaling group. If there is an event then the lambda function will wait for a bit (by default 5 minutes) to give the instance a chance to finish boostrapping before executing the diagnostic checks. Optionally, once the diagnostic checks have finished, the lambda function can terminate all instances that are considered to be orphans based on the output of the checks (this option is turned off by default). The output of these checks can be found in a Cloudwatch log group named `orphan-instance-log-group`.

---

### Example diagnostic check output
Here's what is outputted for a registered EC2 instance
```
Container Instance is registered to an ECS Cluster.
```

Here's what is outputted for an unregistered EC2 instance
```
Warning! Container instance is not registered to ECS. [Reported Instance ID: i-123]
----Summary----
Register Container Instance Output: level=error time=2024-02-06T22:35:29Z msg="Unable to register as a container instance with ECS" error="ClientException: Cluster not found."
Checking status of essential system services...
* ECS Status: active
* Docker Status: active
* Containerd Status: active
Conducting Docker lifecycle test...
* Docker Image Build Status: Success
* Docker Container Test Run: Success
* Docker Container Exit Code: 1
* Docker Container Error: 
* Docker Container OOM: false
Checking status of ECS Agent...
* Agent Container Exit Code: 1
* Agent Container Error: 
* Agent Container OOM: false
Checking if ECS endpoint is reachable...
* ECS Endpoint Reachability: Able to reach ecs.us-west-1.amazonaws.com
Validating ECS Configuration file...
Checking Nvidia driver status
* Unable to check Nvidia Driver Status. Disregard if the AMI is not GPU-supported, otherwise please ensure that the nvidia-smi library is installed.
----Analysis----
* Agent was unable to start up properly. Please refer to the following doc to obtain agent logs: https://docs.aws.amazon.com/AmazonECS/latest/developerguide/ecs-logs-collector.html.
* Error while running test container . Please refer to the following link regarding docker diagnostics: https://docs.aws.amazon.com/AmazonECS/latest/developerguide/docker-diags.html
```

---

### Set up
1. Download the `setup.sh` script
2. Obtain your AWS account credentials 
3. Note down the name of all of the autoscaling groups that will be tracked.
4. Note down the specific region to deploy all of the resources to.
5. Run the following command:
```
./setup.sh --region us-east-2 --asg-name my-asg --wait-time 120 --enable-cleanup
```
  - Required flags: Region name, Autoscaling group name(s)
  - Optional flags: Wait time duration (in seconds) and Enable instance cleanup
  - Note: You can specify multiple autoscaling groups by using a "," as the delimiter (e.g. myAsg1,myAsg2,...)
  - Note: It will take a bit while it's creating the cloudformation stack.

The `setup.sh` script will do the following:
- Download the cloudformation stack template as well as the lambda function handler as a ZIP file from the public repo.
- Create a new S3 bucket and store the lambda function handler ZIP file.
- Create a new Cloudformation stack from the stack template.
- Cleans up the downloaded files once it's done.

---

### Example output of setup

```
Setting up resources for ECS Orphan Instance Diagnostic Checker...
Downloading Orphan Instance Cloudformation template...
  % Total    % Received % Xferd  Average Speed   Time    Time     Time  Current
                                 Dload  Upload   Total   Spent    Left  Speed
100 12798  100 12798    0     0  71268      0 --:--:-- --:--:-- --:--:-- 71497
Downloading Lambda function handler...
  % Total    % Received % Xferd  Average Speed   Time    Time     Time  Current
                                 Dload  Upload   Total   Spent    Left  Speed
  0     0    0     0    0     0      0      0 --:--:-- --:--:-- --:--:--     0
100 6016k  100 6016k    0     0  11.5M      0 --:--:-- --:--:-- --:--:--  109M
Orphan Instance S3 bucket created.
Copying over lambda function handler ZIP file...
upload: ./myFunction.zip to s3://orphan-instance-us-east-2/myFunction.zip
Creating Cloudformation stack...
{
    "StackId": "arn:aws:cloudformation:us-west-2:123:stack/orphan-instance/123"
}
Cloudfromation stack has been created.
ECS Orphan Instance Dianostic Checker setup finished.
```

---

