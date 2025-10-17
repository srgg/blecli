# MANDATORY: Claude Code Editing Rules

## Documentation anc Comments Validation

**CRITICAL:** ALWAYS validate documentation matches implementation before modifications.
**CRITICAL:** Writing comments is an essential part of code development. Comments must be written as a seasoned senior engineer would — concise, clear, and informative, providing meaningful context without stating the obvious or including trivial details.

### Validation Process

1. **Analyze for mismatches** - Compare documentation to actual code/test behavior
2. **STOP if mismatch found** - DO NOT proceed with changes
3. **Ask user** which to fix:
    - Fix documentation to match code, OR
    - Fix code to match documentation, OR
    - Skip validation for this case
4. **NEVER assume** documentation is correct without verification
5. **NEVER modify** either without explicit user approval when mismatched

**Applies to:**
- Test GOAL/TEST SCENARIO comments
- Function/class documentation
- Code comments describing behavior
- API documentation

## Edit Tool Requirements

**CRITICAL:** ALWAYS use Edit/MultiEdit tools for ALL file modifications. NEVER use alternative editing methods.

### Required Workflow

1. **Read file first** - ALWAYS read before modifications
2. **Use Edit/MultiEdit ONLY** - old_string → new_string format
3. **Batch changes** - ALL related changes in single MultiEdit per file
4. **Verify after** - Read file to confirm changes

**Why Edit/MultiEdit ONLY:**
- Preserves exact formatting and indentation
- Follows Claude Code established patterns
- Maintains project consistency
- User expects standard behavior

### ❌ FORBIDDEN Tools

**NEVER use these for file editing:**

```bash
# Symbol editors
replace_symbol_body, insert_after_symbol, insert_before_symbol

# JetBrains tools
replace_text_in_file, insert_text_at_caret

# Bash editing
sed, awk, echo >, cat >, patch, rm

# Git patch operations (except git mv)
git add -p, git reset --patch
```

### ✅ ALLOWED Operations

```bash
# File moves (version-controlled files)
git mv <old> <new>
```

## Code Design Principles

**MANDATORY:** Apply these principles to ALL code changes:

### Core Principles
- **Single Responsibility:** ONE purpose per class/function/module
- **Open/Closed:** Extend behavior WITHOUT modifying existing code
- **Liskov Substitution:** Subclasses replaceable for base classes
- **Interface Segregation:** Small, specific interfaces over large ones
- **Dependency Inversion:** Depend on abstractions, NEVER concrete implementations
- **KISS:** Implement simplest solution that works
- **DRY:** NO code duplication - extract shared logic immediately
- **Fail Fast:** Validate inputs early, error on invalid states
- **YAGNI:** Implement ONLY requested features, no extras
- **Low Coupling:** Independent components, maximum reusability

### ❌ PROHIBITED Practices

**NEVER:**
- Over-engineer solutions
- Create unused abstractions
- Prematurely optimize
- Add unrequested features
- Duplicate code

## Tool Usage Separation

- **Analysis tools:** Reading and understanding code ONLY
- **Edit/MultiEdit tools:** ALL file modifications

## Enforcement

These rules are **NON-NEGOTIABLE**. Violations will result in rejected changes.