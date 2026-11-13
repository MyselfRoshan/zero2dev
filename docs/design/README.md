# Design files

Zero2Dev's dashboard looks and behaves like Ant Design, but it is written in Svelte. These files explain the look to people and to AI tools.

| File | What it is for |
| --- | --- |
| [../../DESIGN.md](../../DESIGN.md) | **Zero2Dev's design language.** Tokens in the front matter (colours, type, radius, spacing), then the rules: colours and the accent, contrast, typography, layout, components, do's and don'ts, theming. Start here for any UI change. |
| [ant-design.md](ant-design.md) | Ant Design's own `design.md`, copied unchanged from https://ant.design/design.md (MIT licence). It is the upstream reference for anything `DESIGN.md` does not cover. Where the two differ, `DESIGN.md` wins: for example, Zero2Dev keeps text at WCAG AA contrast. |

The values in `DESIGN.md` are generated in code by [web/src/lib/theme.ts](../../web/src/lib/theme.ts). The test `web/src/lib/design-md.test.ts` fails if the two drift apart.

## Ant Design's AI resources

Ant Design publishes more material for AI tools. It describes the **React components**, which Zero2Dev does not use. Read it for how a component should look and behave (states, sizes, spacing, DOM structure), not for code to copy.

| Link | Content |
| --- | --- |
| https://ant.design/docs/react/design-md | What `design.md` is and how to use it |
| https://ant.design/llms.txt | An index of all antd docs |
| https://ant.design/llms-full.txt | Full component documentation with examples |
| https://ant.design/llms-semantic.md | Each component's parts and DOM structure |

A single component's page is the docs URL with `.md` added, for example https://ant.design/components/button.md.

## Updating the vendored copy

Download https://ant.design/design.md again and replace the content of `ant-design.md` below its note, keeping the front matter first. Then update the "Fetched" date in the note. If antd's tokens changed, check whether `theme.ts` and `DESIGN.md` should follow.
