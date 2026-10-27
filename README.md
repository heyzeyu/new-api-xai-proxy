# new-api-xai-proxy

面向 [new-api](https://github.com/QuantumNous/new-api) 的独立 xAI 兼容反代任务插件。插件标识为 `xai-proxy`，显示名称为 **xAI Proxy**。它基于 QuantumNous/new-api 的 xAI 任务插件适配，不覆盖内置 xAI 插件。

## 安装

在「任务插件 → 插件市场」添加索引：

```text
https://raw.githubusercontent.com/heyzeyu/new-api-xai-proxy/main/marketplace.json
```

也可以在「上传插件 → 从 URL 导入」填写固定版本：

```text
https://raw.githubusercontent.com/heyzeyu/new-api-xai-proxy/v0.1.1/plugin.js
```

索引包含固定版本的源码 SHA-256。插件也可直接上传仓库中的 `plugin.js`。宿主须支持 Task Plugin API v1、模型用量配置、原生任务路由和标准视频 / Responses 协议。

安装后新建「任务插件」类型的渠道，选择 **xAI Proxy**，填入自己的反代 Base URL 和上游密钥，再选择需要开放的模型及用户分组。示例默认地址仅用于标识 xAI 接口；实际反代地址由管理员配置，不随源码发布。

## 模型与接口

插件声明 `grok-imagine-video`、`grok-imagine-video-1.5`、`grok-imagine-video-1.5-lite`。声明表示插件具备该模型的参数解析逻辑，具体反代是否提供、支持哪些输入、可用时长和实际输出效果仍以上游为准。

```bash
curl "$GATEWAY_URL/v1/videos" \
  -H "Authorization: Bearer $GATEWAY_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"model":"grok-imagine-video","prompt":"A blue ball rolls on a table","seconds":"1","resolution":"480p","aspect_ratio":"16:9"}'
```

使用部署者网关的 API Key，勿将反代的上游密钥提供给客户端。提交返回网关任务 ID；`GET /v1/videos/{id}` 查询状态，成功时 `content.video_url` 指向网关结果地址，`GET /v1/videos/{id}/content` 可用网关令牌下载视频。宿主的任务媒体接口也可以提供带访问能力的链接。

- `POST /v1/responses`：支持同步、流式进度和 `background: true`。
- `POST /xai-proxy/v1/videos/generations`：原生视频生成。
- `POST /xai-proxy/v1/videos/edits`：原生视频编辑（基础模型）。
- `POST /xai-proxy/v1/videos/extensions`：原生视频延长（基础模型）。
- `GET /xai-proxy/v1/videos/{request_id}`：查询网关持久化的任务。

当反代将完成的视频返回为 `/v1/videos/{上游任务ID}/content` 时，插件在 HTTPS 渠道地址上发送带渠道密钥的下载请求。它只允许与当前任务完全匹配的路径；公网视频 URL 不携带上游密钥。若媒体无效或无法读取，编辑和延长请求保留原先预留的清晰度档位，不猜测结果。

## 计费与边界

插件仅提供宿主需要的用量事实，不携带特定账户的价格或折扣。管理员应在 new-api 配置按模型、分辨率、输出秒数、输入图片及输入视频秒数计费的表达式。多个插件提供相同模型时，可按 `xai-proxy::<模型名>` 设置独立的提供商价格；否则沿用宿主已有的模型价格。预留、结算和退款由宿主管理。

编辑与延长的输出分辨率通过视频文件头探测；无法探测时保留预留档位。媒体 URL 的过期时间、上游审核、账号权限及接口兼容性由具体反代决定。插件参数校验和模拟响应测试不代表每个反代或每个模型参数均已真实测试。

## 开发与验证

Node.js 22+，无额外 Node 依赖：

```bash
npm run check
```

CI 固定到公开 new-api 提交 `206e0fa5c600127a314a97f88592c067f784f4a6`，使用真实宿主 JavaScript 引擎回放合成用例，检查与内置 xAI 路由共存、价格事实和受保护视频的分辨率探测。测试不包含真实密钥或账户数据。

在 new-api 源码目录中运行宿主校验：

```bash
go run /path/to/new-api-xai-proxy/tools/contract/main.go \
  -plugin /path/to/new-api-xai-proxy/plugin.js \
  -fixture /path/to/new-api-xai-proxy/test/contract-fixture.json
```

更新源码或版本后运行 `npm run marketplace`，提交并创建对应版本标签。

## 来源与许可

本插件基于 [QuantumNous/new-api](https://github.com/QuantumNous/new-api) 的 xAI 任务插件和 Task Plugin API v1，保留原项目及作者署名。AGPL-3.0-only，见 `LICENSE` 和 `NOTICE`。本项目由社区维护，并非 xAI 或任何反代服务商的官方 SDK；开发由 OpenAI Codex 辅助完成。
