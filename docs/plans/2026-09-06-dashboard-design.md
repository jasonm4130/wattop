# Dashboard presentation

Give wattop a mactop-inspired instrument panel that makes hardware load and
coding sessions easy to scan together. This spec guides the presentation change;
collectors, pricing, and the JSON contract remain unchanged.

Use a restrained palette, labelled borders, aligned meters, and a compact
identity bar. Wide terminals group compute and memory beside power, cooling,
and I/O. Narrow terminals retain a compact hardware strip. Give the session
table a section label, muted column headings, and a themed selection.

Verify the rendered frame at 80, 120, and 160 columns, including short windows,
empty sessions, and no-colour mode. Run the existing Go tests and build, then
exercise the binary in a real terminal against local collectors. Keep unknown
readings and estimated values explicitly labelled.
