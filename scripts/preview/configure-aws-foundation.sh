#!/bin/sh
# Sets up the durable AWS prerequisites for PR previews. The Preview Lambda
# uses the existing production Lambda OPENROUTER_API_KEY value.
set -eu

region=${AWS_REGION:-ap-northeast-1}
account_id=017719539487
rds_master_secret_arn='arn:aws:secretsmanager:ap-northeast-1:017719539487:secret:rds!db-f473bfeb-9bbf-43ef-81b5-1333e34b8e15-GqEM8u'
task_execution_role=profile-preview-ecs-task-execution-role
task_role=profile-preview-ecs-task-role
bootstrap_execution_role=profile-preview-bootstrap-task-execution-role
lambda_execution_role=profile-preview-lambda-execution-role
codebuild_role=profile-preview-codebuild-service-role

ecs_trust='{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"ecs-tasks.amazonaws.com"},"Action":"sts:AssumeRole"}]}'
lambda_trust='{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"lambda.amazonaws.com"},"Action":"sts:AssumeRole"}]}'

ensure_role() {
  name=$1 trust=$2
  if aws iam get-role --role-name "$name" >/dev/null 2>&1; then
    aws iam update-assume-role-policy --role-name "$name" --policy-document "$trust"
  else
    aws iam create-role --role-name "$name" --assume-role-policy-document "$trust" >/dev/null
  fi
}

ensure_role "$task_execution_role" "$ecs_trust"
ensure_role "$task_role" "$ecs_trust"
ensure_role "$bootstrap_execution_role" "$ecs_trust"
ensure_role "$lambda_execution_role" "$lambda_trust"

aws iam put-role-policy --role-name "$task_execution_role" --policy-name preview-task-execution \
  --policy-document "{\"Version\":\"2012-10-17\",\"Statement\":[{\"Effect\":\"Allow\",\"Action\":[\"ecr:GetAuthorizationToken\",\"ecr:BatchCheckLayerAvailability\",\"ecr:GetDownloadUrlForLayer\",\"ecr:BatchGetImage\",\"logs:CreateLogStream\",\"logs:PutLogEvents\"],\"Resource\":\"*\"},{\"Effect\":\"Allow\",\"Action\":\"secretsmanager:GetSecretValue\",\"Resource\":\"arn:aws:secretsmanager:$region:$account_id:secret:profile-learning/postgres/pr-*\"}]}"
aws iam put-role-policy --role-name "$bootstrap_execution_role" --policy-name preview-bootstrap-execution \
  --policy-document "{\"Version\":\"2012-10-17\",\"Statement\":[{\"Effect\":\"Allow\",\"Action\":[\"ecr:GetAuthorizationToken\",\"ecr:BatchCheckLayerAvailability\",\"ecr:GetDownloadUrlForLayer\",\"ecr:BatchGetImage\",\"logs:CreateLogStream\",\"logs:PutLogEvents\"],\"Resource\":\"*\"},{\"Effect\":\"Allow\",\"Action\":\"secretsmanager:GetSecretValue\",\"Resource\":[\"$rds_master_secret_arn\",\"arn:aws:secretsmanager:$region:$account_id:secret:profile-learning/postgres/pr-*\"]}]}"
aws iam put-role-policy --role-name "$lambda_execution_role" --policy-name preview-lambda-execution \
  --policy-document '{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["logs:CreateLogGroup","logs:CreateLogStream","logs:PutLogEvents","ec2:CreateNetworkInterface","ec2:DescribeNetworkInterfaces","ec2:DescribeVpcs","ec2:DescribeSubnets","ec2:DescribeSecurityGroups","ec2:DeleteNetworkInterface"],"Resource":"*"}]}'
aws iam put-role-policy --role-name "$codebuild_role" --policy-name preview-image-build \
  --policy-document "{\"Version\":\"2012-10-17\",\"Statement\":[{\"Sid\":\"WritePreviewBuildLogs\",\"Effect\":\"Allow\",\"Action\":[\"logs:CreateLogGroup\",\"logs:CreateLogStream\",\"logs:PutLogEvents\"],\"Resource\":\"*\"},{\"Sid\":\"GetECRLoginToken\",\"Effect\":\"Allow\",\"Action\":\"ecr:GetAuthorizationToken\",\"Resource\":\"*\"},{\"Sid\":\"PushAndReadPreviewImages\",\"Effect\":\"Allow\",\"Action\":[\"ecr:BatchCheckLayerAvailability\",\"ecr:CompleteLayerUpload\",\"ecr:DescribeImages\",\"ecr:InitiateLayerUpload\",\"ecr:PutImage\",\"ecr:UploadLayerPart\",\"ecr:BatchGetImage\"],\"Resource\":\"arn:aws:ecr:$region:$account_id:repository/profile-api\"}]}"

codebuild_policy="{\"Version\":\"2012-10-17\",\"Statement\":[{\"Sid\":\"PreviewOrchestration\",\"Effect\":\"Allow\",\"Action\":[\"ecs:RegisterTaskDefinition\",\"ecs:RunTask\",\"ecs:DescribeTasks\",\"ecs:DescribeServices\",\"ecs:CreateService\",\"ecs:UpdateService\",\"ecs:DeleteService\",\"ecs:DescribeTaskDefinition\",\"ecs:TagResource\",\"servicediscovery:ListNamespaces\",\"servicediscovery:ListServices\",\"servicediscovery:CreateService\",\"servicediscovery:DeleteService\",\"servicediscovery:TagResource\",\"secretsmanager:CreateSecret\",\"secretsmanager:DescribeSecret\",\"secretsmanager:DeleteSecret\",\"secretsmanager:GetRandomPassword\",\"secretsmanager:TagResource\",\"lambda:CreateFunction\",\"lambda:GetFunction\",\"lambda:GetPolicy\",\"lambda:ListTags\",\"lambda:UpdateFunctionCode\",\"lambda:UpdateFunctionConfiguration\",\"lambda:DeleteFunction\",\"lambda:AddPermission\",\"lambda:GetFunctionConfiguration\",\"lambda:TagResource\",\"apigateway:GET\",\"apigateway:POST\",\"apigateway:DELETE\"],\"Resource\":\"*\"},{\"Sid\":\"PassPreviewTaskRoles\",\"Effect\":\"Allow\",\"Action\":\"iam:PassRole\",\"Resource\":[\"arn:aws:iam::$account_id:role/$task_execution_role\",\"arn:aws:iam::$account_id:role/$task_role\",\"arn:aws:iam::$account_id:role/$bootstrap_execution_role\"],\"Condition\":{\"StringEquals\":{\"iam:PassedToService\":\"ecs-tasks.amazonaws.com\"}}},{\"Sid\":\"PassPreviewLambdaRole\",\"Effect\":\"Allow\",\"Action\":\"iam:PassRole\",\"Resource\":\"arn:aws:iam::$account_id:role/$lambda_execution_role\",\"Condition\":{\"StringEquals\":{\"iam:PassedToService\":\"lambda.amazonaws.com\"}}}]}"
aws iam put-role-policy --role-name "$codebuild_role" --policy-name preview-orchestration --policy-document "$codebuild_policy"

task_execution_role_arn="arn:aws:iam::$account_id:role/$task_execution_role"
task_role_arn="arn:aws:iam::$account_id:role/$task_role"
bootstrap_execution_role_arn="arn:aws:iam::$account_id:role/$bootstrap_execution_role"
lambda_execution_role_arn="arn:aws:iam::$account_id:role/$lambda_execution_role"
openrouter_api_key=$(aws lambda get-function-configuration --function-name profile-api-lambda --region "$region" --query 'Environment.Variables.OPENROUTER_API_KEY' --output text)
test -n "$openrouter_api_key" && test "$openrouter_api_key" != None
env_json=$(jq -nc --arg a "$task_execution_role_arn" --arg b "$task_role_arn" --arg c "$bootstrap_execution_role_arn" --arg d "$lambda_execution_role_arn" --arg e "$rds_master_secret_arn" --arg f '017719539487.dkr.ecr.ap-northeast-1.amazonaws.com/profile-api@sha256:8ad07e989cf5b5718eac1bafb7cb7920e4270a96f9edb2f4e27655ceea889b98' --arg g 'profile-learning-postgres.cho60yiysby4.ap-northeast-1.rds.amazonaws.com' --arg h profile_db --arg i 'subnet-0d1a01ac72b25d263,subnet-088cdcc8a15361762' --arg j sg-0c4eb0b867cabf018 --arg k 'subnet-0d1a01ac72b25d263,subnet-088cdcc8a15361762' --arg l sg-0c23c04ebd777b600 --arg m 'arn:aws:acm:ap-northeast-1:017719539487:certificate/a165ae20-af40-470b-8544-d85981d5e32e' --arg n "$openrouter_api_key" '[[$a,$b,$c,$d,$e,$f,$g,$h,$i,$j,$k,$l,$m,$n] | to_entries[] | {name:(["PREVIEW_TASK_EXECUTION_ROLE_ARN","PREVIEW_TASK_ROLE_ARN","PREVIEW_BOOTSTRAP_TASK_EXECUTION_ROLE_ARN","PREVIEW_LAMBDA_ROLE_ARN","RDS_MASTER_SECRET_ARN","BOOTSTRAP_IMAGE_URI","RDS_ENDPOINT","DATABASE_NAME","APP_SUBNETS","PREVIEW_ECS_SECURITY_GROUP","LAMBDA_SUBNETS","LAMBDA_SECURITY_GROUP","PREVIEW_ACM_CERTIFICATE_ARN","OPENROUTER_API_KEY"][.key]), value:.value, type:"PLAINTEXT"}]')
project_input=$(jq -nc --argjson vars "$env_json" '{name:"profile-preview-deploy",environment:{type:"LINUX_CONTAINER",image:"aws/codebuild/amazonlinux-x86_64-standard:6.0",computeType:"BUILD_GENERAL1_MEDIUM",privilegedMode:true,environmentVariables:$vars}}')
aws codebuild update-project --cli-input-json "$project_input" >/dev/null

printf '%s\n' 'AWS Preview foundation configured.'
