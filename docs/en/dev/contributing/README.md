# Development Collaboration Guide

This directory defines the development workflow shared by contributors and coding agents. [AGENTS.md](../../../../AGENTS.md) is the mandatory reading index; its referenced standards have the same authority. Find product implementation documents through the [development index](../README.md), and production deployment instructions in the [administrator guide](../../admin/README.md).

## Reading Order

| Stage | Document | Contents |
| --- | --- | --- |
| Prepare content | [Public content and internal materials](../releasing/public-content.md#public-content-and-internal-materials) | Committable content, private materials, and publication review |
| Prepare the environment | [Development environment](environment.md) | Machine directories, shared download caches, and development services |
| Start a task | [Collaboration workflow](workflow.md) | Worktrees, branches, validation, commits, merging, and cleanup |
| Verify and deliver | [Acceptance and service activation](acceptance.md) | WebUI links, desktop development packages, model tests, Runtime and main service verification |
| Maintain documentation | [Documentation conventions](documentation.md) | Content ownership, translation, links, and website publishing |
| Maintain CI | [Gitea CI](gitea-ci.md) | Action mirrors, runners, and separate installation acceptance |

## Ownership of Standards

Frontend visuals, copy, and interaction are maintained in the [design system](../frontend/design-system.md) and [interaction conventions](../frontend/interaction.md). Build publication, public content, and licensing belong to [release maintenance](../releasing/releases.md), [public content standards](../releasing/public-content.md), and [license standards](../releasing/licensing.md). This directory links to those standards; each rule is maintained in the document responsible for its topic.
