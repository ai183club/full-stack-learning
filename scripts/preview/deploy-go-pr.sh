#!/bin/sh
# Idempotent AWS half of a PR preview. It deliberately contains no production
# Lambda inspection and no Cloudflare credentials.
set -eu

: "${PR_NUMBER:?}"; : "${GIT_SHA:?}"; : "${IMAGE_URI:?}"
: "${PREVIEW_TASK_EXECUTION_ROLE_ARN:?}"; : "${PREVIEW_TASK_ROLE_ARN:?}"
: "${PREVIEW_BOOTSTRAP_TASK_EXECUTION_ROLE_ARN:?}"; : "${PREVIEW_LAMBDA_ROLE_ARN:?}"
: "${RDS_MASTER_SECRET_ARN:?}"; : "${BOOTSTRAP_IMAGE_URI:?}"; : "${RDS_ENDPOINT:?}"; : "${DATABASE_NAME:?}"
: "${APP_SUBNETS:?}"; : "${PREVIEW_ECS_SECURITY_GROUP:?}"; : "${LAMBDA_SUBNETS:?}"; : "${LAMBDA_SECURITY_GROUP:?}"
: "${PREVIEW_ACM_CERTIFICATE_ARN:?}"; : "${OPENROUTER_API_KEY:?}"; : "${LAMBDA_ZIP_FILE:?}"
AWS_REGION=${AWS_REGION:-ap-northeast-1}; ECS_CLUSTER=${ECS_CLUSTER:-profile-learning-cluster}
eval "$(PR_NUMBER=$PR_NUMBER GIT_SHA=$GIT_SHA sh scripts/preview/pr-context.sh)"
tag_map="Project=profile-learning,Environment=pr-$pr_number,ManagedBy=github-actions"
secret_tags="Key=Project,Value=profile-learning Key=Environment,Value=pr-$pr_number Key=ManagedBy,Value=github-actions"
ecs_tags="key=Project,value=profile-learning key=Environment,value=pr-$pr_number key=ManagedBy,value=github-actions"
namespace_id=$(aws servicediscovery list-namespaces --region "$AWS_REGION" --query 'Namespaces[?Name==`app.internal`].Id|[0]' --output text)
test -n "$namespace_id" && test "$namespace_id" != None

secret_arn=$(aws secretsmanager describe-secret --secret-id "$database_secret_name" --region "$AWS_REGION" --query ARN --output text 2>/dev/null || true)
if [ -z "$secret_arn" ] || [ "$secret_arn" = None ]; then
 generated_password=$(aws secretsmanager get-random-password --password-length 40 --exclude-punctuation --require-each-included-type --query RandomPassword --output text)
 generated_secret=$(jq -nc --arg username "$database_role" --arg password "$generated_password" '{username:$username,password:$password}')
 secret_arn=$(aws secretsmanager create-secret --name "$database_secret_name" --region "$AWS_REGION" --secret-string "$generated_secret" --tags $secret_tags --query ARN --output text)
fi
cloud_map_arn=$(aws servicediscovery list-services --region "$AWS_REGION" --filters "Name=NAMESPACE_ID,Values=$namespace_id,Condition=EQ" --query "Services[?Name=='$cloud_map_service'].Arn|[0]" --output text)
if [ -z "$cloud_map_arn" ] || [ "$cloud_map_arn" = None ]; then cloud_map_arn=$(aws servicediscovery create-service --region "$AWS_REGION" --name "$cloud_map_service" --namespace-id "$namespace_id" --dns-config "NamespaceId=$namespace_id,RoutingPolicy=MULTIVALUE,DnsRecords=[{Type=A,TTL=10}]" --health-check-custom-config FailureThreshold=1 --tags "$tag_map" --query Service.Arn --output text); fi

# Bootstrap schema before a service can use its isolated credentials.
bootstrap=$(aws ecs register-task-definition --region "$AWS_REGION" --family "profile-db-bootstrap-pr-$pr_number" --network-mode awsvpc --requires-compatibilities FARGATE --cpu 256 --memory 512 --execution-role-arn "$PREVIEW_BOOTSTRAP_TASK_EXECUTION_ROLE_ARN" --container-definitions "[{\"name\":\"bootstrap\",\"image\":\"$BOOTSTRAP_IMAGE_URI\",\"essential\":true,\"environment\":[{\"name\":\"PGHOST\",\"value\":\"$RDS_ENDPOINT\"},{\"name\":\"PGPORT\",\"value\":\"5432\"},{\"name\":\"PGDATABASE\",\"value\":\"$DATABASE_NAME\"},{\"name\":\"PR_NUMBER\",\"value\":\"$pr_number\"}],\"secrets\":[{\"name\":\"PGUSER\",\"valueFrom\":\"$RDS_MASTER_SECRET_ARN:username::\"},{\"name\":\"PGPASSWORD\",\"valueFrom\":\"$RDS_MASTER_SECRET_ARN:password::\"},{\"name\":\"PREVIEW_DB_PASSWORD\",\"valueFrom\":\"$secret_arn:password::\"}]}]" --query taskDefinition.taskDefinitionArn --output text)
task=$(aws ecs run-task --region "$AWS_REGION" --cluster "$ECS_CLUSTER" --task-definition "$bootstrap" --launch-type FARGATE --network-configuration "awsvpcConfiguration={subnets=[$APP_SUBNETS],securityGroups=[$PREVIEW_ECS_SECURITY_GROUP],assignPublicIp=DISABLED}" --query 'tasks[0].taskArn' --output text); aws ecs wait tasks-stopped --region "$AWS_REGION" --cluster "$ECS_CLUSTER" --tasks "$task"
test "$(aws ecs describe-tasks --region "$AWS_REGION" --cluster "$ECS_CLUSTER" --tasks "$task" --query 'tasks[0].containers[0].exitCode' --output text)" = 0

# Task definition is constructed, rather than copied from production, so only preview values enter it.
td=$(mktemp); trap 'rm -f "$td"' EXIT
jq -n --arg family "profile-api-pr-$pr_number" --arg image "$IMAGE_URI" --arg exec "$PREVIEW_TASK_EXECUTION_ROLE_ARN" --arg role "$PREVIEW_TASK_ROLE_ARN" --arg schema "$schema" --arg cors "https://$web_host" --arg secret "$secret_arn" '{family:$family,networkMode:"awsvpc",requiresCompatibilities:["FARGATE"],cpu:"256",memory:"512",executionRoleArn:$exec,taskRoleArn:$role,containerDefinitions:[{name:"profile-api",image:$image,essential:true,portMappings:[{containerPort:8080,protocol:"tcp"}],environment:[{name:"APP_ENV",value:"preview"},{name:"DATABASE_SCHEMA",value:$schema},{name:"CORS_ALLOWED_ORIGINS",value:$cors}],secrets:[{name:"DATABASE_USER",valueFrom:($secret+":username::")},{name:"DATABASE_PASSWORD",valueFrom:($secret+":password::")}]}]}' > "$td"
task_def=$(aws ecs register-task-definition --region "$AWS_REGION" --cli-input-json "file://$td" --query taskDefinition.taskDefinitionArn --output text)
if [ "$(aws ecs describe-services --region "$AWS_REGION" --cluster "$ECS_CLUSTER" --services "$ecs_service" --query 'services[0].status' --output text 2>/dev/null || true)" = ACTIVE ]; then aws ecs update-service --region "$AWS_REGION" --cluster "$ECS_CLUSTER" --service "$ecs_service" --task-definition "$task_def" --force-new-deployment >/dev/null; else aws ecs create-service --region "$AWS_REGION" --cluster "$ECS_CLUSTER" --service-name "$ecs_service" --task-definition "$task_def" --desired-count 1 --launch-type FARGATE --platform-version LATEST --network-configuration "awsvpcConfiguration={subnets=[$APP_SUBNETS],securityGroups=[$PREVIEW_ECS_SECURITY_GROUP],assignPublicIp=DISABLED}" --service-registries "registryArn=$cloud_map_arn" --tags $ecs_tags >/dev/null; fi
aws ecs wait services-stable --region "$AWS_REGION" --cluster "$ECS_CLUSTER" --services "$ecs_service"

lambda_environment=$(jq -nc --arg base "http://$cloud_map_service.app.internal:8080" --arg key "$OPENROUTER_API_KEY" '{Variables:{PROFILE_API_BASE_URL:$base,OPENROUTER_API_KEY:$key}}')
if aws lambda get-function --region "$AWS_REGION" --function-name "$lambda_function" >/dev/null 2>&1; then
 aws lambda update-function-code --region "$AWS_REGION" --function-name "$lambda_function" --zip-file "fileb://$LAMBDA_ZIP_FILE" >/dev/null
 aws lambda update-function-configuration --region "$AWS_REGION" --function-name "$lambda_function" --timeout 60 --memory-size 256 --vpc-config "SubnetIds=$LAMBDA_SUBNETS,SecurityGroupIds=$LAMBDA_SECURITY_GROUP" --environment "$lambda_environment" >/dev/null
else
 aws lambda create-function --region "$AWS_REGION" --function-name "$lambda_function" --runtime nodejs22.x --handler index.handler --role "$PREVIEW_LAMBDA_ROLE_ARN" --timeout 60 --memory-size 256 --zip-file "fileb://$LAMBDA_ZIP_FILE" --vpc-config "SubnetIds=$LAMBDA_SUBNETS,SecurityGroupIds=$LAMBDA_SECURITY_GROUP" --environment "$lambda_environment" --tags "$tag_map" >/dev/null
fi
aws lambda wait function-updated --region "$AWS_REGION" --function-name "$lambda_function"
api_id=$(aws apigatewayv2 get-apis --region "$AWS_REGION" --query "Items[?Name=='$api_gateway_name'].ApiId|[0]" --output text)
if [ -z "$api_id" ] || [ "$api_id" = None ]; then api_id=$(aws apigatewayv2 create-api --region "$AWS_REGION" --name "$api_gateway_name" --protocol-type HTTP --cors-configuration "AllowOrigins=https://$web_host,AllowMethods=GET,POST,PATCH,OPTIONS,AllowHeaders=content-type,authorization" --tags "$tag_map" --query ApiId --output text); fi
integration=$(aws apigatewayv2 get-integrations --region "$AWS_REGION" --api-id "$api_id" --query "Items[?IntegrationUri==\`$(aws lambda get-function --region "$AWS_REGION" --function-name "$lambda_function" --query Configuration.FunctionArn --output text)\`].IntegrationId|[0]" --output text)
lambda_arn=$(aws lambda get-function --region "$AWS_REGION" --function-name "$lambda_function" --query Configuration.FunctionArn --output text)
if [ -z "$integration" ] || [ "$integration" = None ]; then integration=$(aws apigatewayv2 create-integration --region "$AWS_REGION" --api-id "$api_id" --integration-type AWS_PROXY --integration-uri "$lambda_arn" --payload-format-version 2.0 --query IntegrationId --output text); fi
route=$(aws apigatewayv2 get-routes --region "$AWS_REGION" --api-id "$api_id" --query "Items[?RouteKey=='\\$default'].RouteId|[0]" --output text); if [ -z "$route" ] || [ "$route" = None ]; then aws apigatewayv2 create-route --region "$AWS_REGION" --api-id "$api_id" --route-key '$default' --target "integrations/$integration" >/dev/null; fi
aws lambda add-permission --region "$AWS_REGION" --function-name "$lambda_function" --statement-id "apigw-$api_id" --action lambda:InvokeFunction --principal apigateway.amazonaws.com --source-arn "arn:aws:execute-api:$AWS_REGION:*:$api_id/*" 2>/dev/null || true
domain=$(aws apigatewayv2 get-domain-names --region "$AWS_REGION" --query "Items[?DomainName=='$api_host'].DomainName|[0]" --output text); if [ -z "$domain" ] || [ "$domain" = None ]; then aws apigatewayv2 create-domain-name --region "$AWS_REGION" --domain-name "$api_host" --domain-name-configurations "CertificateArn=$PREVIEW_ACM_CERTIFICATE_ARN,EndpointType=REGIONAL,SecurityPolicy=TLS_1_2" --tags "$tag_map" >/dev/null; fi
mapping=$(aws apigatewayv2 get-api-mappings --region "$AWS_REGION" --domain-name "$api_host" --query "Items[?ApiId=='$api_id'].ApiMappingId|[0]" --output text); if [ -z "$mapping" ] || [ "$mapping" = None ]; then aws apigatewayv2 create-api-mapping --region "$AWS_REGION" --domain-name "$api_host" --api-id "$api_id" --stage '$default' >/dev/null; fi
target=$(aws apigatewayv2 get-domain-name --region "$AWS_REGION" --domain-name "$api_host" --query 'DomainNameConfigurations[0].ApiGatewayDomainName' --output text)
printf 'api_host=%s\napi_gateway_target=%s\nlambda=%s\n' "$api_host" "$target" "$lambda_function"
