# Profile API Lambda

这是 API Gateway 和私有 Go ECS Service 之间的最小 Lambda 代理。源码在项目中维护；不在 Lambda Console 编辑代码。

当前转发以下路径：

- `/health`
- `/ready`
- `POST /api/profiles`
- `GET /api/profiles/{username}`
- `PATCH /api/profiles/{username}`

并提供由Lambda编排的业务接口：

- `POST /api/profiles/generate-bio`

请求只包含：

```json
{
  "name": "Alice"
}
```

Lambda会规范化Name并生成确定性的内部username，先查询现有Profile。已存在时直接返回；不存在时调用OpenRouter生成Bio，生成独立随机密码，并复用Go的 `POST /api/profiles`保存。并发创建返回409时，Lambda会重新读取已经成功保存的Profile。新接口的成功响应只包含 `name`和`bio`，不暴露内部username或随机密码。

Lambda 通过环境变量 `PROFILE_API_BASE_URL` 访问 Cloud Map DNS 名称：

```text
http://profile-api.app.internal:8080
```

它不持有数据库凭据，也不连接 RDS。

OpenRouter配置只属于Lambda运行环境：

```text
OPENROUTER_API_KEY=<server-side secret>
OPENROUTER_MODEL=google/gemma-4-31b-it:free
```

`OPENROUTER_MODEL`可省略，以上模型是代码默认值。API Key不得写入Web环境变量、Git仓库或请求日志。由于模型请求超时为20秒，部署时Lambda Function timeout至少配置为30秒。

Lambda启用模型推理，但不额外传递输出或推理 token 预算、也不要求上游隐藏 reasoning；这样与已验证的 OpenRouter 请求保持一致。Bio 长度由提示词、Lambda 和 Go 的500个 Unicode 字符校验共同保证。仅当OpenRouter返回429、502、503或504时，在同一个20秒总超时内进行一次受控重试；401等配置或客户端错误不会重试。

当前Prompt要求模型只返回自然、流畅的简体中文Bio，并禁止虚构年龄、性别、职业、学历、雇主、所在地、具体成就或其他敏感事实。该规则只影响首次生成；数据库中已存在的Bio不会重新生成。

## 构建与手动部署

从项目根目录执行：

```bash
pnpm --filter lambda build
pnpm --filter lambda deploy:manual
```

构建产物为 `apps/lambda/dist/profile-api-lambda.zip`。`deploy:manual` 会通过已登录的 AWS CLI profile `profile-learning-local` 上传该 ZIP 到现有 Lambda Function `profile-api-lambda`。

首次手动学习时，也可以在 Lambda Console 使用 **Upload from → .zip file** 上传同一个产物；Function handler 保持 `index.handler`。

## 请求边界

- 请求体最多 `16 KiB`。
- `bio` 非必填，最多 `500` 个 Unicode 字符。
- PATCH 必须包含 `Authorization`；Lambda 只转发认证头，真实验密仍由 Go 完成。
- Go上游请求超时为5秒，OpenRouter请求超时为28秒；为API Gateway HTTP API的30秒同步集成上限预留余量。超时映射为HTTP 504，其他上游网络错误映射为502。
- 日志只记录安全的路径和超时状态，不记录请求正文、密码或 Authorization。
- Lambda 不读取数据库 Secret，不连接 RDS。
- OpenRouter的 `reasoning_details`不会返回给Web或写入数据库，只提取最终assistant文本作为Bio。
- OpenRouter失败日志只包含错误类别和HTTP状态码，不包含API Key、Provider响应正文或用户Name。

## 本地验证

```bash
pnpm --filter lambda test
pnpm --filter lambda check-types
pnpm --filter lambda build
```
