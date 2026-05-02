# Design System Specification: The Terminal Monolith

## 1. Overview & Creative North Star
**Creative North Star: The Precision Instrument**
This design system rejects the "SaaS-standard" aesthetic in favor of a high-density, editorial approach tailored for the elite engineer. It is inspired by high-end audio hardware and aerospace telemetry interfaces. The goal is to move away from generic "blocks" and toward a unified, monolithic architecture where information density is a feature, not a flaw.

We achieve this through **Tonal Architecture**. Instead of relying on borders to separate content, we use subtle shifts in dark-scale values to define hierarchy. The interface should feel like a single, carved slab of graphite where the data—not the container—is the hero.

---

## 2. Colors & Surface Philosophy
The palette is built on a foundation of "Inky Neutrals" punctuated by a "Surgical Cyan" accent.

### Surface Hierarchy & Nesting (The "No-Line" Rule)
Sectioning must be achieved through background shifts, not 1px solid lines.
- **Base Level:** `surface` (#131313) is used for the global background.
- **Primary Containers:** Use `surface_container_low` (#1c1b1b) for large layout blocks.
- **Nested Content:** Use `surface_container` (#20201f) or `surface_container_high` (#2a2a2a) to lift interactive elements like cards or data tables.
- **Active Focus:** `surface_container_highest` (#353535) is reserved for active states or hovered list items.

### Signature Accents
- **Primary:** `primary` (#44d8f1) and `primary_container` (#00bcd4) are used sparingly for critical actions and active indicators.
- **Success/Warning/Danger:** Use `tertiary` (#78dc77) for health, `amber` (custom mapping) for warnings, and `error` (#ffb4ab) for critical failures.

### The Glass & Texture Rule
Floating elements (modals, dropdowns, command palettes) must use **Glassmorphism**. 
- **Token:** `surface_variant` (#353535) at 70% opacity.
- **Effect:** `backdrop-filter: blur(12px)`. This ensures the terminal-like density behind the element remains visible but non-distracting.

---

## 3. Typography: Editorial Technicality
The system pairs the neutrality of **Inter** with the structural precision of a **Monospace** stack.

- **Editorial Hierarchy:** Use `display-sm` for high-level fleet stats, but keep `title-sm` (1rem) as the primary workhorse for server names to maintain density.
- **The Data Layer:** All technical data (IP addresses, UUIDs, Latency, Up-time) must use a Monospaced font at `label-md` or `body-sm`. 
- **Chinese Typography:** When using Chinese labels (e.g., 状态, 负载), ensure a weight of 400 for regular and 600 for emphasis to match the optical weight of Inter.

---

## 4. Elevation & Depth
Depth is defined by light, not shadows.

- **The Layering Principle:** Place a `surface_container_lowest` (#0e0e0e) card inside a `surface_container_low` (#1c1b1b) section to create a "recessed" or "inset" feel.
- **Ambient Shadows:** For floating dialogs, use a 32px blur shadow with 8% opacity of the `on_surface` color. It should feel like an ambient glow, not a drop shadow.
- **The Ghost Border Fallback:** If a container requires a border (e.g., status pill), use `outline_variant` (#3c494c) at **20% opacity**. Never use 100% opaque borders.

---

## 5. Components

### Status Pills (The Fleet Indicators)
These are the pulse of the system. They use a "Ghost Border" and a subtle background tint.
- **Normal (运行中):** Text: `tertiary`, BG: `on_tertiary_container` (15% opacity).
- **Attention (需注意):** Text: `#ffb300` (Amber), BG: Amber (10% opacity).
- **Alert (警报):** Text: `error`, BG: `error_container` (20% opacity).
- **Critical (严重故障):** Text: `#ffffff`, BG: `error` (Solid).
- **Maintenance (维护中):** Text: `secondary`, BG: `secondary_container`.
- **Paused (已暂停):** Text: `outline`, BG: `surface_container_highest`.
- **Archived (已归档):** Text: `on_surface_variant`, BG: `none`, Border: `outline_variant` (20%).

### Buttons
- **Primary:** `primary_container` background with `on_primary_container` text. Radius: `md` (0.375rem).
- **Tertiary (Ghost):** No background. `primary` text. Use `surface_container_highest` on hover.

### Input Fields & Terminal Inputs
- **Base:** `surface_container_lowest` background. 
- **Font:** Monospaced.
- **Indicator:** A 2px left-border of `primary` when focused, rather than a full outline.

### Data Tables (The Grid)
- **Rule:** Forbid horizontal divider lines. 
- **Separation:** Use `4px` of vertical whitespace (`body-sm` height) and alternating row backgrounds using `surface_container_low` and `surface_container`.

---

## 6. Do’s and Don’ts

### Do
- **Do** prioritize information density. Engineers want to see 50 servers on one screen, not 5.
- **Do** use `letter-spacing: -0.01em` on Inter for a more premium, "tighter" feel.
- **Do** include Chinese labels in a slightly smaller scale (`label-sm`) when used as secondary descriptors.

### Don’t
- **Don't** use 100% white (#ffffff) for text. Always use `on_surface` (#e5e2e1) to reduce eye strain in dark mode.
- **Don't** use standard Material Design "Floating Action Buttons." Keep actions inline with the Tonal Architecture.
- **Don't** use rounded corners larger than `lg` (0.5rem). The system should feel architectural and rigid, not bubbly.