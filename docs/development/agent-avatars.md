# Agent avatars

Personal-agent appearance uses the shared AvatarSpec contract. The existing
world silhouettes remain the basis for 14 × 14 portraits and 16 × 28 sprites.
This feature supplies design and rendering primitives; agent persistence and
chat selection use the agent registry separately.

`internal/agents.ValidateAvatar(spec)` returns a normalized spec or an error.
It requires the six supported variants, six accessory values, and six complete
hex colors. Colors are canonical uppercase. Skin too dark for the fixed dark
eyes falls back to warm skin at a contrast ratio of at least 3:1. Hair too close
to skin receives a contrasting fallback at a ratio of at least 1.5:1. Other
palette choices remain as supplied. The web applies the same corrections;
malformed stored specs render a safe default instead of failing the view.

`DefaultAvatar(seed)` hashes the trimmed, lowercase name to choose a coordinated
palette and silhouette. It accepts an empty seed and needs no random state.

The deferred `design_avatar` tool accepts `description` and an optional complete
`spec`. The model translates a character description into fields; the tool
validates those choices and returns `{spec, preview}` without saving an agent.
A description-only call returns a deterministic starting avatar. The tool is
fast, read-only, and available through the default and delegate catalogs, with
English and Spanish intent keywords. It and the `agent-avatars` skill are
excluded from eval. Adding the tool changes the default tool-name hash only;
the fixed prefix and eval profile hash remain pinned.

Web callers import `AgentAvatar` and `AvatarSpec` from
`apps/web/src/components/AgentAvatar.tsx`:

```tsx
<AgentAvatar spec={agent.avatar} size={40} label={agent.name} />
<AgentAvatar spec={agent.avatar} size={96} form="sprite" label={agent.name} />
```

The default form is a portrait. `size` gives its square size or a full sprite's
height; `frame` selects the existing sprite pose. The caller supplies a localized
accessible label. Without a label the image is decorative. SVG unit pixels use
crisp edges and pixelated scaling for sizes from 20 to 96 CSS pixels.

Tone colors skin; outfit colors clothing, hood and cap; hair, highlight, accent
and background remain independent. Glasses, sunglasses, an earring, scarf and
star render as pixel overlays on every silhouette in both forms. Existing
`PixelSprite` and `PixelPortrait` callers keep their clothing meaning of `tone`
and their previous pixel grids.

Glasses leave skin visible beside each eye inside the frames. With the smaller
frames, the eye pixels filled the entire lens opening and made glasses look
opaque. A regression test checks the clear lenses on every silhouette in both
forms.

`AgentAvatar.gallery.tsx` is a development fixture covering all 36 combinations
in both forms at 20, 40 and 96 pixels. Vitest renders the complete gallery and
checks palette mapping, each overlay, pose preservation and invalid-spec
fallback. Legacy grid hashes cover every variant, six frames and six actions.
Shared fixtures in `internal/agents/testdata/avatar_cases.json` ensure the Go
and TypeScript normalizers agree. Go tests also cover color and enum rejection,
contrast across grayscale values, stable defaults, tool execution, cancellation,
intent activation, skill validation and eval isolation.
