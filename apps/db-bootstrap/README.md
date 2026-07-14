# Database Bootstrap Task

这是一次性 Fargate Task 使用的数据库初始化容器，不是 ECS Service。它只通过私有 App子网和 `profile-ecs-sg`连接 RDS，并执行以下可重复操作：

- 创建或更新应用数据库角色 `profile_app`；
- 创建 `profiles`表；
- 授予应用角色对该表和其 ID sequence所需的最小 CRUD权限。

它还会撤销 PostgreSQL 默认 `PUBLIC` 对 `public` Schema、其中表及 sequence的权限，再仅向 `profile_app`显式授予所需权限。这是 PR Preview 独立数据库角色不能访问生产 `public` Schema 的前置条件。

容器从环境变量接收 RDS 主账号和应用账号密码。密码必须通过 ECS `ValueFrom`引用 Secrets Manager 注入，禁止写入 Task Definition、命令行或日志。

## 镜像构建与推送

从项目根目录执行：

```bash
docker build --platform linux/arm64 \
  --tag profile-db-bootstrap:manual-001 \
  apps/db-bootstrap

docker tag profile-db-bootstrap:manual-001 \
  017719539487.dkr.ecr.ap-northeast-1.amazonaws.com/profile-api:db-bootstrap-001

docker push \
  017719539487.dkr.ecr.ap-northeast-1.amazonaws.com/profile-api:db-bootstrap-001
```

首次运行前，Docker 需要先登录 ECR；完整登录命令见 [Go API README](../api/README.md#推送镜像到-amazon-ecr)。

## 运行时要求

```text
PGHOST                 RDS endpoint
PGPORT                 5432
PGDATABASE             已创建的目标数据库名
PGUSER                 RDS主账号用户名（通过 Secret 注入）
PGPASSWORD             RDS主账号密码（通过 Secret 注入）
PROFILE_APP_USER       profile_app
PROFILE_APP_PASSWORD   应用账号密码（通过 Secret 注入）
PGSSLMODE              verify-full
PGSSLROOTCERT          /usr/local/share/ca-certificates/rds-global-bundle.pem
```

任务成功日志只会出现 `database bootstrap started` 和 `database bootstrap completed`，不会输出密码。
