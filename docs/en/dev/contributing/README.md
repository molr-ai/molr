# Development Collaboration Guide

This directory defines the development workflow shared by contributors and coding agents. [AGENTS.md](../../../../AGENTS.md) is the mandatory reading index; its referenced standards have the same authority. Find product implementation documents through the [development index](../README.md), and production deployment instructions in the [administrator guide](../../admin/README.md).

## Design and Delivery Principles

Handle the complexity ourselves; keep things simple for users. Design, implementation, and delivery should reduce what users need to understand, configure, and do, while following the KISS principle.

- Use sensible defaults, automatic initialization, and automation to handle necessary complexity so common tasks take as few steps as possible.
- Provide commands that can be run directly or complete configurations for installation, deployment, and updates. Let the software perform preparation it can automate, so users do not have to assemble commands manually or understand internal implementation details.
- Offer advanced configuration as needed. Interfaces and documentation should prioritize the information required for the current task and clearly explain choices users need to make and their effects.
- During acceptance, walk through the entire flow as a first-time user and keep removing unnecessary steps, repeated input, and extra dependencies.

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
