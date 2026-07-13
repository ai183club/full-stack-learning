# Go API

## 第一课：连接本地 PostgreSQL

项目根目录启动数据库：

```bash
docker compose up -d postgres
docker compose ps
```

PostgreSQL 在容器内监听 `5432`，映射到宿主机的 `5433`，避免与本机已有的 PostgreSQL 冲突。

第一次创建新项目的数据表：

```bash
docker exec -it full-stack-learning-postgres \
  psql -U profile_user -d profile_db
```

进入 `psql` 后手动执行：

```sql
CREATE TABLE profiles (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    username VARCHAR(32) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    name VARCHAR(80) NOT NULL,
    bio TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

查看表结构并退出：

```text
\d profiles
\q
```

运行 Go 数据库连接检查：

```bash
cd apps/api
set -a
source .env
set +a
go run ./cmd/checkdb
```

成功输出：

```text
数据库连接成功：localhost:5433/profile_db
```

停止数据库：

```bash
docker compose stop postgres
```

`docker compose down -v` 会同时删除数据库数据卷，不要在希望保留数据时执行。

## 第二课：使用 Go 按用户名查询

先加载本地环境变量：

```bash
cd apps/api
set -a
source .env
set +a
```

查询用户名为 `henry` 的 Profile：

```bash
go run ./cmd/findprofile henry
```

查询代码使用参数占位符：

```sql
SELECT id, username, password_hash, name, bio, created_at, updated_at
FROM profiles
WHERE username = $1;
```

`$1` 的值由 `pgx` 单独传给 PostgreSQL，不把用户输入拼接进 SQL。这可以避免 SQL 注入。

`password_hash` 在 Go 结构体中标记为 `json:"-"`，所以查询结果转换成 JSON 时不会泄露密码哈希。

## 第三课：启动 HTTP API

加载环境变量并启动服务：

```bash
cd apps/api
set -a
source .env
set +a
go run ./cmd/server
```

打开另一个终端验证接口：

```bash
curl http://localhost:8080/health
curl http://localhost:8080/ready
curl http://localhost:8080/api/profiles/henry
```

- `/health` 只检查 Go 进程是否正在运行，未来供容器做基础存活检查。
- `/ready` 会 Ping PostgreSQL，确认服务具备处理数据库请求的条件。
- `/api/profiles/{username}` 使用路径中的用户名查询 Profile。

按 `Ctrl+C` 停止服务。程序收到 `SIGINT` 或 `SIGTERM` 后会执行优雅停机；ECS 更新或停止 Fargate Task 时会用到这个机制。

## 第四课：注册 Profile

启动 API 后发送注册请求：

```bash
curl -i -X POST http://localhost:8080/api/profiles \
  -H 'Content-Type: application/json' \
  --data '{
    "username": "alice",
    "password": "learning-password",
    "name": "Alice",
    "bio": "正在学习 Go 后端开发。"
  }'
```

成功时返回 `201 Created`。相同用户名再次注册返回 `409 Conflict`。

输入规则：

- `username`：3-32 位，只允许小写英文字母、数字和下划线。
- `password`：8-72 字节。
- `name`：1-80 个字符。
- `bio`：可选，不限制字数。

密码在 Service 层使用 bcrypt 生成哈希，Repository 只把哈希写入 PostgreSQL。API 响应不会包含原密码或 `password_hash`。

## 第五课：修改个人信息

使用 `PATCH` 可以只更新提交的字段：

```bash
curl -i -X PATCH http://localhost:8080/api/profiles/alice \
  -H 'Content-Type: application/json' \
  --data '{
    "name": "Alice Chen",
    "bio": "正在学习 Go、PostgreSQL 和 AWS。"
  }'
```

只更新 `name`：

```json
{"name":"Alice Chen"}
```

只更新或清空 `bio`：

```json
{"bio":""}
```

请求必须至少包含 `name` 或 `bio`。用户不存在时返回 `404 Not Found`，更新成功返回最新的 Profile 和 `updatedAt`。

修改接口使用 HTTP Basic Authentication。`curl -u` 会生成 `Authorization: Basic ...` 请求头：

```bash
curl -i -u alice:learning-password \
  -X PATCH http://localhost:8080/api/profiles/alice \
  -H 'Content-Type: application/json' \
  --data '{"name":"Alice Authenticated","bio":"通过身份认证修改。"}'
```

Go 会从请求头读取用户名和密码，确认认证用户名与 URL 中的用户名一致，再使用 bcrypt 对比输入密码和数据库中的 `password_hash`。

以下情况统一返回 `401 Unauthorized`：

- 没有提供认证信息。
- 用户名或密码错误。
- 使用一个账号尝试修改另一个账号。

Basic Auth 每次请求都会携带用户名和密码，因此生产环境必须使用 HTTPS。这个项目后续由 ALB/API Gateway 提供 TLS；现阶段只用于本地学习最基础的认证过程。

## Docker 镜像

在项目根目录构建镜像：

```bash
docker build --tag profile-api:local apps/api
```

镜像使用多阶段构建，最终层只包含 Linux Go 二进制、RDS CA证书和最小运行环境。它以非 root用户运行，并包含 `/health`容器健康检查。

在本地 Docker网络中连接 PostgreSQL：

```bash
docker run --rm --name profile-api-local \
  --network full-stack-learning_default \
  --publish 8080:8080 \
  --env APP_ENV=development \
  --env HTTP_PORT=8080 \
  --env DATABASE_HOST=full-stack-learning-postgres \
  --env DATABASE_PORT=5432 \
  --env DATABASE_USER=profile_user \
  --env DATABASE_PASSWORD=profile_password \
  --env DATABASE_NAME=profile_db \
  --env DATABASE_SSL_MODE=disable \
  profile-api:local
```

ECS连接 RDS时使用：

```text
DATABASE_SSL_MODE=verify-full
DATABASE_SSL_ROOT_CERT=/app/certs/global-bundle.pem
```

本地构建镜像的平台是 `linux/arm64`，因此后续 ECS Fargate Task Definition选择 ARM64。

## 推送镜像到 Amazon ECR

这一步把本地构建的 Go API 镜像上传到私有 ECR 仓库，后续 ECS Fargate Task 从这里拉取同一个不可变版本运行。它不上传源码，也不包含数据库密码。

已使用的仓库和区域：

```text
Region: ap-northeast-1
Repository: profile-api
Repository URI: 017719539487.dkr.ecr.ap-northeast-1.amazonaws.com/profile-api
```

前提：本机已安装 Docker 和 AWS CLI，并且已完成临时凭证登录：

```bash
aws login --profile profile-learning-local
```

先确认当前凭证属于预期 AWS 账号：

```bash
aws sts get-caller-identity --profile profile-learning-local
```

登录 Docker 到该区域的 ECR Registry。登录令牌短期有效，失效后重新执行此命令：

```bash
aws ecr get-login-password \
  --region ap-northeast-1 \
  --profile profile-learning-local \
| docker login \
  --username AWS \
  --password-stdin 017719539487.dkr.ecr.ap-northeast-1.amazonaws.com
```

构建、标记并推送一个明确版本。不要把部署版本只标为 `latest`：

```bash
docker build --platform linux/arm64 --tag profile-api:manual-001 apps/api

docker tag profile-api:manual-001 \
  017719539487.dkr.ecr.ap-northeast-1.amazonaws.com/profile-api:manual-001

docker push \
  017719539487.dkr.ecr.ap-northeast-1.amazonaws.com/profile-api:manual-001
```

本次已推送版本的不可变镜像引用为：

```text
017719539487.dkr.ecr.ap-northeast-1.amazonaws.com/profile-api@sha256:70997c6dd04a8db5409a2d194e8451324df99fba4114457acb287fd475bd629b
```

后续 ECS Task Definition 将使用这个 digest，而不是可变标签。每次发布新版本时，替换 `manual-001` 为新的唯一标签，并记录 ECR 返回的新 digest。
