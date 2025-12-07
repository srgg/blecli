# 🚨 CRITICAL: Claude Code Serena MCP Requirements - MUST FOLLOW

## ⚠️ MANDATORY COMPLIANCE - NO EXCEPTIONS

**CRITICAL RULE**: Claude Code MUST ALWAYS use Serena MCP server tools for ANY codebase search, analysis, or exploration operations. This is REQUIRED for proper operation and CANNOT be ignored.

### 🔒 REQUIRED Tools for ALL Codebase Operations:

1. **Code Search**: `mcp__serena__search_for_pattern` - NEVER use Grep tool
2. **File Listing**: `mcp__serena__list_dir` - NEVER use Bash ls commands  
3. **Symbol Analysis**: `mcp__serena__get_symbols_overview` - ALWAYS check file structure first
4. **Symbol Search**: `mcp__serena__find_symbol` - ALWAYS use for finding functions/classes/methods
5. **Reference Finding**: `mcp__serena__find_referencing_symbols` - ALWAYS use for usage analysis
6. **File Finding**: `mcp__serena__find_file` - NEVER use Bash find commands

### ❌ STRICTLY PROHIBITED Tools for Codebase Operations:
- ❌ Grep tool (FORBIDDEN - use `mcp__serena__search_for_pattern`)
- ❌ Glob tool for code (FORBIDDEN - use `mcp__serena__find_file`)
- ❌ Bash ls commands (FORBIDDEN - use `mcp__serena__list_dir`)
- ❌ Bash find commands (FORBIDDEN - use `mcp__serena__find_file`)
- ❌ Read tool for entire files without first using `mcp__serena__get_symbols_overview`

### 🔥 CRITICAL: Code Editing Tools - ALWAYS Use Standard Edit Tools
- ✅ **ALWAYS use Edit tool** for single code modifications
- ✅ **ALWAYS use MultiEdit tool** for multiple changes
- ✅ **ALWAYS use Write tool** for new files
- ❌ **NEVER use `mcp__serena__replace_symbol_body`** - use Edit tool instead
- ❌ **NEVER use `mcp__serena__insert_after_symbol`** - use Edit tool instead
- ❌ **NEVER use `mcp__serena__insert_before_symbol`** - use Edit tool instead

### 🎯 MANDATORY Workflow for Code Operations:

1. **ALWAYS** start with `mcp__serena__get_symbols_overview` for new files
2. **ALWAYS** use `mcp__serena__find_symbol` with `include_body=true` for targeted reads
3. **ALWAYS** use `mcp__serena__search_for_pattern` for pattern searches
4. **NEVER** read entire files without first exploring with symbol tools
5. **ALWAYS** use standard Edit/Write/MultiEdit tools for code modifications

### 🔥 Critical Benefits - Why This is MANDATORY:
- **Semantic Understanding**: Serena provides actual code comprehension
- **Token Efficiency**: Dramatically reduces unnecessary token usage
- **Symbol Relationships**: Tracks function/class dependencies properly
- **Architecture Compliance**: Required for Claude Code's design
- **Performance**: Superior analysis capabilities vs basic text tools

### ✅ Only Use Standard Tools For:
- **File editing**: Edit, Write, MultiEdit (ALWAYS use these for code changes)
- **Build/test operations**: Bash for make, go test, etc.
- **Non-code file operations**
- **System commands unrelated to code analysis**

### 🚨 VIOLATION CONSEQUENCES:
Failure to follow these requirements will result in:
- Inefficient token usage
- Poor code understanding
- Missed symbol relationships
- Suboptimal development experience

**THIS IS NOT OPTIONAL - THESE ARE CORE REQUIREMENTS FOR CLAUDE CODE OPERATION**