# Molr

产品官网：`molr.ai` · [GitHub 项目](https://github.com/molr-ai/molr)

许可证：[MIT](LICENSE)，适用于 Molr 自有代码和文档。第三方组件保留各自许可证，授权范围与分发条件见 [第三方声明](THIRD_PARTY_NOTICES.md)。

Molr 是可自托管的个人 AI 助理，优先让日常委托可信、执行可靠、成本有边界。通过持续对话交代事情，在计划中查看安排与执行结果，并按助理管理资料、模型和预算。

当前提供连接真实 Go Runtime 的 WebUI、独立产品官网，以及使用模拟数据的 UI Design。Runtime 已实现持久化、认证授权、业务模块、Agent 执行和 Linux 命令沙盒。桌面与手机浏览器可连接同一服务；UI Design 的 Desktop／Mobile／CLI 预览不等于独立原生客户端已发布。

## 使用 Molr

- [产品文档](docs/README.md)：快速开始、助理与计划、资源权限、预算及排障。
- [架构总览](docs/architecture.md)：组件构成、执行链路、沙盒边界与数据存放。
- [部署与维护](docs/deployment/README.md)：静态构建、初始化、HTTPS、服务、备份恢复与升级。
- [Linux 命令环境](docs/deployment/sandbox.md)：依赖、授权、运行限制与能力检查。

本地部署不等于所有推理离线；使用远程模型或网络服务时，相关输入可能发送到对应服务。具体能力以实际部署、模型及授权为准。

## 参与开发

先阅读 [项目协作约定](AGENTS.md)，开发、测试和文档修改都在独立 worktree 完成。当前产品范围、架构、API、品牌及协作规范统一在 [开发文档](dev-docs/README.md)。

```text
molr.ai/
├── docs/                # 面向用户的产品与部署文档
├── dev-docs/            # 公开开发与维护文档
├── apps/
│   ├── webui/           # 连接真实 Runtime 的产品 WebUI
│   ├── website/         # 独立产品官网
│   ├── ui-design/       # 品牌、组件和模拟交互展示
│   └── ui-example/      # UI Host 开发参考，未开放产品安装
├── packages/
│   ├── sdk/             # Runtime 客户端、DTO 与 UI/Host 协议
│   ├── ui/              # 官方共享组件、设计变量与 Logo
│   └── ui-host/         # 内置主题解析与隔离适配基础
└── runtime/             # Go 单体服务与测试
```

## 本地开发与预览

使用根目录 `package.json` 要求的 Node.js、`runtime/.go-version` 指定的 Go，以及用于构建检查的 Python 3（仅标准库）。各 worktree 使用独立依赖、数据和端口；以下为默认端口，已有服务占用时需另选空闲端口。

```bash
npm ci
npm run dev:webui       # WebUI：5191，需要自己的 Runtime
npm run dev:website     # 官网：5189
npm run dev             # UI Design：5188，模拟数据
```

前端默认监听 `0.0.0.0`。WebUI 默认代理本机 `127.0.0.1:8080`，独立验收需通过 `MOLR_RUNTIME_URL` 指向同一 worktree 自己的 Runtime，不能复用主环境。Runtime 保留其 TLS 和安全监听要求，详情见 [WebUI 开发说明](apps/webui/README.md) 和 [Runtime README](runtime/README.md)。

```bash
npm run typecheck
npm run build            # 构建所有提供 build 脚本的工作区
npm run test:ui
npm run test:website
npm run test:webui
make -C runtime check
```

浏览器测试需要 Playwright Chromium，按改动范围运行相应检查。官网、UI Design 与 WebUI 的独立启动和预览命令分别见其应用 README；不要把前端开发代理作为公网生产网关。

品牌资源与生成方式见 [品牌规范](dev-docs/frontend/BRAND.md)，组件接口见 [组件文档](packages/ui/COMPONENTS.md)。公开内容与内部资料的存放规则见 [开发规范](dev-docs/DEVELOPMENT.md#公开内容与内部资料)。
