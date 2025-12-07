# Claude Code Editing Rules

**CRITICAL:** ALWAYS follow these rules without exception:

### **MANDATORY** Workflow:
1. ALWAYS read file first before ANY modifications
2. EXCLUSIVELY use Edit/MultiEdit tools for ALL file changes (old_string → new_string format)
3. REQUIRED: Batch ALL related changes in a single MultiEdit operation per file
4. ALWAYS verify changes by reading file after modifications

### Why Edit/MultiEdit ONLY:
- Preserves exact formatting/indentation
- Follows Claude Code established patterns
- User expects standard Claude Code editing behavior
- Maintains consistency with project conventions

### File Operations:
- Rename/Move: MUST use `git mv <old> <new>` for version-controlled files

### FORBIDDEN Tools (NEVER USE):
- Symbol editors: replace_symbol_body, insert_after_symbol, insert_before_symbol
- JetBrains tools: replace_text_in_file, insert_text_at_caret
- Bash file editing: sed, awk, echo >, cat >, patch, rm
- Git patch operations: git add -p, git reset --patch (EXCEPT: git mv for moves)

### STRICT Code Principles:
- Single Responsibility: ONE purpose per class/function/module
- Open/Closed: Extend behavior without modifying existing code
- Liskov Substitution: Subclasses replaceable for their base classes
- Interface Segregation: Small, specific interfaces over large, general ones
- Dependency Inversion: Depend on abstractions, never on concrete implementations 
- KISS: Implement simplest solution that works
- DRY: No code duplication - extract shared logic immediately
- Fail Fast: Validate inputs early, error on invalid states
- YAGNI: Implement ONLY requested features, no extras
- Low Coupling: Independent components, maximum reusability
- PROHIBITED: Over-engineering, unused abstractions, premature optimization

### Tool Usage Separation:
- Analysis tools: For reading and understanding code ONLY
- Edit/MultiEdit tools: For ALL file modifications