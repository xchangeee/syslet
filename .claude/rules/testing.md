---
paths:
  - "**/*_test.go"
  - "**/*testing*.go"
  - "**/*testutil*.go"
  - "**/testing/*.go"
  - "**/testutil/*.go"
---

# Testing Rules

## Shared vocabulary

**Condition segments:**

- State: `<State>` (lifecycle or validity state of the subject)
- Change: `<ChangeType>` (what kind of change triggered the operation)
- Policy/flag: `With<Policy>`, `Without<Policy>`
- Input shape: `<InputVariant>` (path form, empty input, invalid type, etc.)
- Conjunctions: fuse with `And` (`<ConditionA>And<ConditionB>`) or keep as separate segments

**Outcome segments:**

- Pure returns: `Returns<Value>` (e.g. `ReturnsNil`, `ReturnsError`, `ReturnsEmpty`, `ReturnsTrue`)
- Side effects: `<Verb><Object>` (e.g. `WritesFile`, `RemovesFile`, `Starts<X>`, `Stops<X>`)
- No-op: `NoAction`, `NoChange`
- Error path: `Errors`, `DoesNotStart`

Define the project's vocabulary once and prefer it over free-form wording to keep grep-ability across the test suite.


## Test Naming Scheme

Naming is governed by two independent axes: **level** and **structure**.

### Axis 1: Level

**Unit tests** name a concrete subject (function, method, or operation+target):

- `Test<Func>_...`
- `Test<Type>_<Method>_...`
- `Test<Op>_<Target>_...`

**Integration tests** name a scenario instead of a subject. The scenario implies the scope:

- `Test<Scenario>_...`

### Axis 2: Structure

**Single outcome** — condition(s) and outcome are all in the top-level name. Use when there is only one meaningful assertion.

```
Test<Subject>_<Condition>_<Outcome>
```

**Multiple outcomes** — top-level encodes subject + conditions; each `t.Run` names one outcome.

```
Test<Subject>_<Condition>[_<Condition>...]
  t.Run("<Outcome>", ...)
  t.Run("<Outcome>", ...)
```

**Parametrized** — top-level encodes subject + the dimension being varied; each `t.Run` names the specific case. Assertions are inline per case.

```
Test<Subject>_<ConditionGroup>
  t.Run("<SpecificCondition>", ...)
  t.Run("<SpecificCondition>", ...)
```

### Combining the axes

| | Unit | Integration |
|---|---|---|
| Single outcome | `TestStore_Write_FileAbsent_WritesFile` | `TestCheckout_WithExpiredCard_ReturnsError` |
| Multiple outcomes | `TestStore_Write_FileAbsent` → `t.Run("WritesFile", ...)` | `TestCheckout_WithExpiredCard` → `t.Run("ReturnsError", ...)` |
| Parametrized | `TestStore_Write_FileStates` → `t.Run("FileAbsent", ...)` | `TestCheckout_CardStates` → `t.Run("Expired", ...)` |

For integration tests, outcome may be omitted from the top-level name when the scenario makes it self-evident.
