# Shared controls

`Select` and `Segmented` accept translated `label` and option `label` values.
Call `t()` in the feature that owns those strings. Both are controlled through
`value` and `onValueChange`; options contain `value`, `label`, and optional
`disabled` fields. Do not use empty strings as Select option values: Radix
reserves them for clearing the selection and displaying `placeholder`.

Select exposes a combobox trigger and a popover listbox with typeahead,
arrow-key navigation, Home/End, Enter/Space selection, Escape dismissal,
and focus restoration. Pass `name` for form integration. Its portal stays
inside a surrounding native dialog so the modal's top layer and inert
background do not hide or disable the options.

Segmented exposes a horizontal radio group with roving focus, arrow keys,
and disabled-option skipping. Use it for a small set of mutually exclusive
values. The sliding indicator follows the controlled value and respects
reduced motion.

Button supports `primary`, `secondary`, `ghost`, `icon`, and `destructive`
variants. It defaults to `type="button"`; form actions must set `type="submit"`.
Icon-only buttons need an accessible label. Dialog restores the previous
focus and supports Escape and backdrop dismissal.

All visual rules live in `src/styles/redesign/shell.css` and use semantic
palette and motion tokens. Existing feature-local controls remain available
for later consolidation.
