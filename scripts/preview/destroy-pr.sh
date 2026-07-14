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
if [ -n "$secret_arn" ] && [ "$secret_arn" != None ] && [ "${SKIP_PREVIEW_SCHEMA_CLEANUP:-false}" != true ]; then
 cleanup_container=$(jq -nc --arg image "$BOOTSTRAP_IMAGE_URI" --arg host "$RDS_ENDPOINT" --arg database "$DATABASE_NAME" --arg pr "$pr_number" --arg master "$RDS_MASTER_SECRET_ARN" --arg preview "$secret_arn" --arg region "$AWS_REGION" '{name:"cleanup",image:$image,essential:true,entryPoint:["/usr/local/bin/preview-schema.sh"],environment:[{name:"PGHOST",value:$host},{name:"PGPORT",value:"5432"},{name:"PGDATABASE",value:$database},{name:"PGSSLMODE",value:"verify-full"},{name:"PGSSLROOTCERT",value:"/usr/local/share/ca-certificates/rds-global-bundle.pem"},{name:"PR_NUMBER",value:$pr},{name:"PREVIEW_CLEANUP",value:"true"}],secrets:[{name:"PGUSER",valueFrom:($master+":username::")},{name:"PGPASSWORD",valueFrom:($master+":password::")},{name:"PREVIEW_DB_PASSWORD",valueFrom:($preview+":password::")}],logConfiguration:{logDriver:"awslogs",options:{"awslogs-group":"/profile-learning/ecs/db-bootstrap","awslogs-region":$region,"awslogs-stream-prefix":("cleanup-pr-"+$pr)}}}')
 cleanup=$(aws ecs register-task-definition --region "$AWS_REGION" --family "profile-db-cleanup-pr-$pr_number" --network-mode awsvpc --requires-compatibilities FARGATE --runtime-platform cpuArchitecture=ARM64,operatingSystemFamily=LINUX --cpu 256 --memory 512 --execution-role-arn "$PREVIEW_BOOTSTRAP_TASK_EXECUTION_ROLE_ARN" --container-definitions "[$cleanup_container]" --query taskDefinition.taskDefinitionArn --output text)
 cleanup_run=$(aws ecs run-task --region "$AWS_REGION" --cluster "$ECS_CLUSTER" --task-definition "$cleanup" --launch-type FARGATE --network-configuration "awsvpcConfiguration={subnets=[$APP_SUBNETS],securityGroups=[$PREVIEW_ECS_SECURITY_GROUP],assignPublicIp=DISABLED}" --output json)
 task=$(printf '%s' "$cleanup_run" | jq -r '.tasks[0].taskArn // empty')
 if [ -z "$task" ]; then
  printf '%s' "$cleanup_run" | jq '{failures}' >&2
  exit 1
 fi
 aws ecs wait tasks-stopped --region "$AWS_REGION" --cluster "$ECS_CLUSTER" --tasks "$task"
 cleanup_result=$(aws ecs describe-tasks --region "$AWS_REGION" --cluster "$ECS_CLUSTER" --tasks "$task" --output json)
 if [ "$(printf '%s' "$cleanup_result" | jq -r '.tasks[0].containers[0].exitCode // empty')" != 0 ]; then
  printf '%s' "$cleanup_result" | jq '{failures,task:{stopCode:.tasks[0].stopCode,stoppedReason:.tasks[0].stoppedReason,containers:[.tasks[0].containers[]|{name,exitCode,reason}]}}' >&2
  exit 1
 fi
fi
namespace_id=$(aws servicediscovery list-namespaces --region "$AWS_REGION" --query 'Namespaces[?Name==`app.internal`].Id|[0]' --output text)
test -n "$namespace_id" && test "$namespace_id" != None
service_id=$(aws servicediscovery list-services --region "$AWS_REGION" --filters "Name=NAMESPACE_ID,Values=$namespace_id,Condition=EQ" --query "Services[?Name=='$cloud_map_service'].Id|[0]" --output text)
if [ -n "$service_id" ] && [ "$service_id" != None ]; then
 attempt=1
 until aws servicediscovery delete-service --region "$AWS_REGION" --id "$service_id"; do
  if [ "$attempt" -ge 12 ]; then exit 1; fi
  attempt=$((attempt + 1))
  sleep 5
 done
fi
aws secretsmanager delete-secret --region "$AWS_REGION" --secret-id "$database_secret_name" --force-delete-without-recovery 2>/dev/null || true
