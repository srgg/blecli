# CLAUDE.md

## CRITICAL: Session Initialization

**MANDATORY**: At EVERY new Claude Code session start:

1. Initialize Serena MCP
2. Critical: Read and ALWAYS follow critical-engineering-practices.md
3. Read language/context-specific standards based on work type (see below)
2. Read `mandatory-standards-code.md` (ALWAYS required)
3. Read language/context-specific standards based on work type (see below)

## Required Standards by Work Type

### ALWAYS Read
```
.serena/memories/mandatory-standards-code.md
```

### Conditional Reading (based on work context)

**Working with Go code:**
```
.serena/memories/mandatory-standards-go-style.md
```

**Working with C/C++ embedded code:**
```
.serena/memories/mandatory-standards-embedded-cpp-style.md
```

**Working with tests:**
```
.serena/memories/mandatory-standards-testing.md
```

## Session Workflow

**CRITICAL**: Follow this workflow:

1. **Initialize**: Read `mandatory-standards-code.md`
2. **Identify context**: Determine language/work type
3. **Read specific standards**: Load applicable language/test standards
4. **Verify**: Check documentation matches implementation
5. **Modify**: Use Edit/MultiEdit tools exclusively
6. **Apply standards**: Follow all loaded standards
7. **Verify**: Confirm changes comply

## Re-read During Session

**MUST re-read after each major operation:**

- `mandatory-standards-code.md` - After ANY file modification
- Language-specific standard - After code changes in that language
- `mandatory-standards-testing.md` - After ANY test modification

## Enforcement

These requirements are **NON-NEGOTIABLE**. ALL modifications MUST comply with applicable standards.