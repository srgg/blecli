# Troubleshoot Command

Systematically analyze and diagnose test failures with a comprehensive root cause analysis.


## General Workflow

**Critical** Do not make any bold, unrequested guesses.  Do analysis, collecting and analyzing evidence.

1. **MANDATORY**: Ensure the root project [CLAUDE.md](../../CLAUDE.md) and Serena memory [go_test_documentation_standards.md](../../.serena/memories/go_test_documentation_standards.md) is read and followed.
2. **Mandatory** If there is no well-grounded root cause added, add and collect targeted debug logs to clarify and collect evidence:
    — Use proper logging in the code around.
    - Add comments explaining debug purpose: // DEBUG: <purpose> for troubleshooting <issue>
    - Never use fmt.Printf - always use logger.WithFields().Debug()
3 Provide comprehensive assessment:
    - Root cause explanation with evidence
    - Debug log output analysis
    - Recommended fixes (prioritized)
    - Next steps for resolution

## Test Failure Analysis Workflow

    
2. **Create todo list** for systematic tracking:
    - Localize failing test
    - Analyze test failure and root cause
    - Add temporary debug logs if needed
    - Provide assessment and recommendations

2.1**Localize test using Serena tools** (REQUIRED):
    - Use `mcp__serena__search_for_pattern` to find test
    - Use `mcp__serena__get_symbols_overview` for file structure
    - Use `mcp__serena__find_symbol` with `include_body=true` for test implementation

2.2**Run failing test** to capture error output:
   ```bash
   go test -v ./pkg/path -run "TestPattern"

2.3. Analyze dependencies and setup:
   - Use mcp__serena__find_symbol to examine test setup methods
   - Use mcp__serena__find_referencing_symbols to understand relationships
   - Use mcp__serena__search_for_pattern for error messages in codebase
6. Add targeted debug logs if the root cause is unclear:
   - Use proper logging framework (logrus) with structured fields
   - Add comments explaining debug purpose: // DEBUG: <purpose> for troubleshooting <issue>
   - Never use fmt.Printf - always use logger.WithFields().Debug()
7. Re-run test with debug logs to confirm hypothesis
8. Provide comprehensive assessment:
   - Root cause explanation with evidence
   - Debug log output analysis
   - Recommended fixes (prioritized)
   - Next steps for resolution

Example Workflow

# Step 1: Find and analyze test
/troubleshoot-test-failure TestBridge2BLENotifyIntegration

# Step 2: The command will:
# - Read memories and initialize Serena
# - Create systematic todo list
# - Find test using mcp__serena__search_for_pattern
# - Examine test with mcp__serena__find_symbol
# - Run test to capture failure
# - Analyze related code with Serena tools
# - Add debug logs using proper logging
# - Provide root cause analysis with recommendations

Required Behavior

- NEVER use prohibited tools (Grep, Glob, Bash ls/find for code)
- ALWAYS use Serena MCP tools for code analysis
- ALWAYS use Edit tool for modifications, never Serena editing tools
- ALWAYS use proper logging framework (logrus) for debug logs
- ALWAYS add comments explaining debug log purpose
- ALWAYS provide evidence-based root cause analysis
- ALWAYS update todo list progress throughout process

Output Format

## Test Failure Analysis: <TestName>

### Root Cause
[Clear explanation with evidence]

### Debug Evidence
[Relevant log output and findings]

### Recommendations
1. **Fix 1: [Primary solution]** (Recommended)
2. **Fix 2: [Alternative solution]**
3. **Fix 3: [Comprehensive solution]**

### Debug Logs Added
- Location: `file:line`
- Purpose: [Explanation]
- Comment: `// DEBUG: [purpose] for troubleshooting [issue]`

### Next Steps
1. Remove debug logs after implementing fix
2. Implement recommended solution
3. Verify fix with test run

Notes

- Debug logs should be temporary and well-commented
- Use structured logging with proper field names
- Focus on systematic analysis over quick fixes
- Provide actionable recommendations with priority

This slash command encapsulates the systematic troubleshooting approach I used, enforces the proper Claude Code behavior from the memories, and
provides a reusable framework for test failure analysis.



# Troubleshoot Command

Perform a comprehensive troubleshooting assessment

## Scope Determination

**Step 1: Identify the scope using this priority order:**

1. **If arguments are provided** (`#$ARGUMENTS` is not empty):
    - Parse the arguments to identify if they specify a file path, function, test, error, struct, or other language construct
    - Set the identified element as the assessment scope

2. **If no arguments provided**:
    - First check for any selected code in the IDE - if code is selected, use the selection as the scope
    - If no code is selected, use the currently opened file in the IDE as the scope
    - Get the file path and name of the currently active file in the editor

3. **If scope cannot be determined**:
    - Ask the user to clarify the intended scope and request they rerun the command

**CRITICAL: Scope Validation**

You MUST explicitly state which scope was detected and confirm it follows the priority order above. Use this exact format:

```
🎯 **Detected Scope**
Priority applied: [Arguments provided / Selected code / Active file / Cannot determine]
Target: [specific file path, function name, or code selection details]
Reasoning: [brief explanation of why this scope was chosen]
```

**NEVER** assume or expand scope beyond what was detected. If you detect an active file, assess ONLY that file - do not expand to the entire project without explicit user confirmation.

**Important: Handle Uncertainty Clearly**

If there is ANY uncertainty about:
- Which specific file, function, or code construct to analyze
- Whether the detected scope is what the user intended
- How to interpret the provided arguments
- Whether selected code vs opened file should take priority

**Always ask for clarification by:**
1. Clearly stating what is uncertain or ambiguous
2. Explaining what options were detected or considered
3. Asking the user to specify their exact intention
4. Providing examples of how they can clarify (e.g., "Please specify the function name" or "Confirm if you want the entire file analyzed")

**Example uncertainty handling:**
```
🤔 **Scope Uncertainty Detected**
I found multiple possible scopes:
- Selected code: lines 45-67 in auth.go (handleAuth function)
- Opened file: auth.go (entire file)
- Arguments provided: "auth" (could mean auth.go file or auth-related functions)

Please clarify which you want assessed:
- Just the selected function
- The entire auth.go file  
- All auth-related code across the project
```

**Step 2: Confirm scope with user**

Once scope is determined, display the scope details and use an interactive prompt:

```
📋 **Code Quality Assessment Scope**
Target: [specific file path and details]
```

Then use: `#$PROMPT_SELECT("Do you want to proceed with the assessment?", "Yes", "No")`

Only proceed if "Yes" is selected.

## Troubleshooting  Areas

**Step 3: If confirmed, conduct a comprehensive analysis covering:**

Use debug logs if needed to get a clear picture, but include comments explaining why these logs are being retained for further review.

