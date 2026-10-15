## ADDED Requirements

### Requirement: A component children block does not carry a bare expression on one line

A component call whose entire children block is written on one line SHALL NOT
contain a bare expression such as `{ item.Key }`. In that position templ parses
the braces as the children delimiters and the content as raw text, emitting
`templruntime.WriteString(..., "item.Key ")`, so the page renders the source
expression literally. An expression SHALL instead be wrapped in a second pair of
braces (`{ { item.Key } }`) or placed on its own line inside a multi-line block.

A test gate over the generated template output SHALL fail when a raw-text write
carries a code-shaped Go expression — a selector (`item.Key`), a call (`fn(x)`),
or an index (`item[0]`) — anywhere in the gateway, and the component that was
corrected SHALL carry a rendered-output assertion that the expression's value,
not its source text, appears in the output. The gate SHALL fail rather than pass
silently when its scan matches no generated files. This gate scans generated
output on purpose: it asserts a property of the compiled markup, not a
line-mapped finding over authored sources.

#### Scenario: A raw-text expression write is rejected

- **WHEN** a gateway template's generated output writes a code-shaped Go expression as literal text
- **THEN** the gate fails and names the generated file and the expression

#### Scenario: A corrected component renders the value

- **WHEN** a component whose title is supplied as an expression is rendered
- **THEN** the rendered output contains the expression's value
- **AND** the rendered output does not contain the expression's source text

#### Scenario: Prose and accepted expression forms are unaffected

- **WHEN** a children block holds prose, mixed text with an inner expression such as `{ v{ b.Version } draft }`, or an inner expression such as `{ { p.Description.After } }`
- **THEN** the gate does not flag it, because its generated output carries prose or an escaped expression rather than a raw-text expression literal
