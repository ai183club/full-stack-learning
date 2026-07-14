#!/bin/sh
set -eu
: "${PR_NUMBER:?}"; : "${GIT_SHA:?}"; : "${PREVIEW_BOOTSTRAP_TASK_EXECUTION_ROLE_ARN:?}"; : "${RDS_MASTER_SECRET_ARN:?}"; : "${BOOTSTRAP_IMAGE_URI:?}"; : "${RDS_ENDPOINT:?}"; : "${DATABASE_NAME:?}"; : "${APP_SUBNETS:?}"; : "${PREVIEW_ECS_SECURITY_GROUP:?}"
AWS_REGION=${AWS_REGION:-ap-northeast-1}; ECS_CLUSTER=${ECS_CLUSTER:-profile-learning-cluster}
eval "$(PR_NUMBER=$PR_NUMBER GIT_SHA=$GIT_SHA sh scripts/preview/pr-context.sh)"
api_id=$(aws apigatewayv2 get-apis --region "$AWS_REGION" --query "Items[?Name=='$api_gateway_name'].ApiId|[0]" --output text)
mapping=$(aws apigatewayv2 get-api-mappings --region "$AWS_REGION" --domain-name "$api_host" --query "Items[?ApiId=='$api_id'].ApiMappingId|[0]" --output text 2>/dev/null || true)
if [ -n "$mapping" ] && [ "$mapping" != None ]; then aws apigatewayv2 delete-api-mapping --region "$AWS_REGION" --domain-name "$api_host" --api-mapping-id "$mapping"; fi
aws apigatewayv2 delete-domain-name --region "$AWS_REGION" --domain-name "$api_host" 2>/dev/null || true
if [ -n "$api_id" ] && [ "$api_id" != None ]; then aws apigatewayv2 delete-api --region "$AWS_REGION" --api-id "$api_id"; fi
aws lambda delete-function --region "$AWS_REGION" --function-name "$lambda_function" 2>/dev/null || true
if [ "$(aws ecs describe-services --region "$AWS_REGION" --cluster "$ECS_CLUSTER" --services "$ecs_service" --query 'services[0].status' --output text 2>/dev/null || true)" = ACTIVE ]; then aws ecs update-service --region "$AWS_REGION" --cluster "$ECS_CLUSTER" --service "$ecs_service" --desired-count 0 >/dev/null; aws ecs delete-service --region "$AWS_REGION" --cluster "$ECS_CLUSTER" --service "$ecs_service" --force >/dev/null; aws ecs wait services-inactive --region "$AWS_REGION" --cluster "$ECS_CLUSTER" --services "$ecs_service"; fi
secret_arn=$(aws secretsmanager describe-secret --region "$AWS_REGION" --secret-id "$database_secret_name" --query ARN --output text 2>/dev/null || true)
if [ -n "$secret_arn" ] && [ "$secret_arn" != None ]; then
 cleanup=$(aws ecs register-task-definition --region "$AWS_REGION" --family "profile-db-cleanup-pr-$pr_number" --network-mode awsvpc --requires-compatibilities FARGATE --cpu 256 --memory 512 --execution-role-arn "$PREVIEW_BOOTSTRAP_TASK_EXECUTION_ROLE_ARN" --container-definitions "[{\"name\":\"cleanup\",\"image\":\"$BOOTSTRAP_IMAGE_URI\",\"essential\":true,\"environment\":[{\"name\":\"PGHOST\",\"value\":\"$RDS_ENDPOINT\"},{\"name\":\"PGPORT\",\"value\":\"5432\"},{\"name\":\"PGDATABASE\",\"value\":\"$DATABASE_NAME\"},{\"name\":\"PR_NUMBER\",\"value\":\"$pr_number\"},{\"name\":\"PREVIEW_CLEANUP\",\"value\":\"true\"}],\"secrets\":[{\"name\":\"PGUSER\",\"valueFrom\":\"$RDS_MASTER_SECRET_ARN:username::\"},{\"name\":\"PGPASSWORD\",\"valueFrom\":\"$RDS_MASTER_SECRET_ARN:password::\"},{\"name\":\"PREVIEW_DB_PASSWORD\",\"valueFrom\":\"$secret_arn:password::\"}]}]" --query taskDefinition.taskDefinitionArn --output text)
 task=$(aws ecs run-task --region "$AWS_REGION" --cluster "$ECS_CLUSTER" --task-definition "$cleanup" --launch-type FARGATE --network-configuration "awsvpcConfiguration={subnets=[$APP_SUBNETS],securityGroups=[$PREVIEW_ECS_SECURITY_GROUP],assignPublicIp=DISABLED}" --query 'tasks[0].taskArn' --output text); aws ecs wait tasks-stopped --region "$AWS_REGION" --cluster "$ECS_CLUSTER" --tasks "$task"; test "$(aws ecs describe-tasks --region "$AWS_REGION" --cluster "$ECS_CLUSTER" --tasks "$task" --query 'tasks[0].containers[0].exitCode' --output text)" = 0
fi
namespace_id=$(aws servicediscovery list-namespaces --region "$AWS_REGION" --query 'Namespaces[?Name==`app.internal`].Id|[0]' --output text)
test -n "$namespace_id" && test "$namespace_id" != None
service_id=$(aws servicediscovery list-services --region "$AWS_REGION" --filters "Name=NAMESPACE_ID,Values=$namespace_id,Condition=EQ" --query "Services[?Name=='$cloud_map_service'].Id|[0]" --output text); if [ -n "$service_id" ] && [ "$service_id" != None ]; then aws servicediscovery delete-service --region "$AWS_REGION" --id "$service_id"; fi
aws secretsmanager delete-secret --region "$AWS_REGION" --secret-id "$database_secret_name" --force-delete-without-recovery 2>/dev/null || true
