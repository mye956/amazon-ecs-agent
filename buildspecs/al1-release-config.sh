#!/bin/bash

GITHUB_DEST_BRANCH_NAME=$(echo ${CODEBUILD_WEBHOOK_BASE_REF} | cut -d "/" -f3-)
GIT_COMMIT_SHA=$(git rev-parse $GITHUB_DEST_BRANCH_NAME)
GIT_COMMIT_SHORT_SHA=$(git rev-parse --short=8 $GITHUB_DEST_BRANCH_NAME)
RELEASE_DATE=$(date +'%Y%m%d')
AGENT_VERSION=$(cat VERSION)


cat << EOF > agent.json
{
    "agentReleaseVersion" : "$AGENT_VERSION",
    "releaseDate" : "$RELEASE_DATE",
    "initStagingConfig": {
    "release": "1"
    },
    "agentStagingConfig": {
    "releaseGitSha": "$GIT_COMMIT_SHA",
    "releaseGitShortSha": "$GIT_COMMIT_SHORT_SHA",
    "gitFarmRepoName": "MadisonContainerAgentExternal",
    "gitHubRepoName": "aws/amazon-ecs-agent",
    "gitFarmStageBranch": "v${AGENT_VERSION}-stage",
    "githubReleaseUrl": ""
    }
}
EOF

# Downloading the agentVersionV2-<branch>.json (it is assumed that it already exists)
aws s3 cp ${RESULTS_BUCKET_URI}/agentVersionV2/agentVersionV2-${GITHUB_SOURCE_BRANCH_NAME}.json ./agentVersionV2-${GITHUB_SOURCE_BRANCH_NAME}.json
# Grabbing the new current and release agent version
CURR_VER=$(jq -r '.releaseAgentVersion' agentVersionV2-${GITHUB_SOURCE_BRANCH_NAME}.json)

if [[ ! $AGENT_VERSION =~ "${CURR_VER}" ]] ; then
    echo "New Agent release version ${AGENT_VERSION}."
    # Creating temp file with the updated existing agentVersion-<branch>.json file
    cat <<< $(jq '.releaseAgentVersion = '\"$AGENT_VERSION\"' | .currentAgentVersion = '\"$CURR_VER\"'' agentVersionV2-${GITHUB_SOURCE_BRANCH_NAME}.json) > agentVersionV2-${GITHUB_SOURCE_BRANCH_NAME}-COPY.json
    # Replace existing agentVersion-<branch>.json with the with the updated version
    jq . agentVersionV2-${GITHUB_SOURCE_BRANCH_NAME}-COPY.json > agentVersionV2-${GITHUB_SOURCE_BRANCH_NAME}.json
fi

# Uploading new agentVersionV2-<branch>.json and agent.json files
aws s3 cp ./agentVersionV2-${GITHUB_SOURCE_BRANCH_NAME}.json ${RESULTS_BUCKET_URI}/agentVersionV2/agentVersionV2-${GITHUB_SOURCE_BRANCH_NAME}.json
aws s3 cp ./agent.json ${RESULTS_BUCKET_URI}/${GITHUB_SOURCE_BRANCH_NAME}/agent.json