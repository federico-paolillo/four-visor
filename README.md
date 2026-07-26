# 4Visor

> 4Visor is a read-only anonymous Progressive Web App that presents 4chan
> through a modern, content-focused interface while preserving the ordering,
> content and philosophy of the original platform.

## Disclaimer

I don't own any content, trademarks, names, whatever. I just happen to make
viewer to present data available online in a way I like. I don't moderate,
change, own or otherwise have any association with whatever data I display and
cache. If you have complaints go complain to the actual owners. You cannot blame
a pair of binoculars for showing you something you don't wish to see.

## AI Disclosure

AI and related technologies bring strong opinions from many people. To ensure
transparency I will disclose that this repository is mainly developed using LLM
agents (specifically OpenAI Codex). I am one person and I don't have enough wake
time to follow my projects. I will tell that I personally setup the project
structure, [`README.md`](README.md). I personally designed the architecture,
constraints and deployment models. I have also thouroughly supervised the agent
output, ensuring it matches my expecatation. You can say I have indeed vibecoded
this project but you cannot say I have not designed and architected the project.
All automatic guardrails (linters, formatters, etc.) have been configured and
prepared personally by me.

## Getting started

> Use [Mise-en-Place](https://mise.jdx.dev) to setup all necessary components.

- `backend/` contains backend code. Written in Go
- `frontend/` containes frontend code. Written in TypeScript

## Verification

### Backend

> Run these Mise-en-Place tasks to verify backend

- `fe:lint`
- `fe:typecheck`
- `fe:build`
- `fe:test`

### Frontend

> Run these Mise-en-Place tasks to verify frontend

- `be:build`
- `be:lint`
- `be:test`
