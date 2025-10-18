# Code Quality Assessment Command

Perform a comprehensive code quality assessment for source code, automated tests, and whatever is requested, and analyze multiple dimensions of code health and production readiness.

## Scope Determination

**Step 1: Identify the scope using this priority order:**

1. **If arguments are provided** (`#$ARGUMENTS` is not empty):
    - Parse the arguments to identify if they specify a file path, function, struct, or other language construct
    - Set the identified element as the assessment scope

2. **If no arguments provided**:
    - First check for any selected code in the IDE - if code is selected, use the selection as the scope
    - If no code is selected, use the currently opened file in the IDE as the scope
    - Get the file path and name of the currently active file in the editor

3. **If scope cannot be determined**:
    - Ask the user to clarify the intended scope and request that they rerun the command

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
- Which specific file, function, or code construct to analyze?
- Whether the detected scope is what the user intended?
- How to interpret the provided arguments?
- Whether selected code vs. an opened file should take priority?

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

Once the scope is determined, display the scope details and use an interactive prompt:

```
📋 **Code Quality Assessment Scope**
Target: [specific file path and details]
```

Then use: `#$PROMPT_SELECT("Do you want to proceed with the assessment?", "Yes", "No")`

Only proceed if "Yes" is selected.

## Assessment Areas

**Step 3: If confirmed, conduct a comprehensive analysis covering:**

### **Code Quality & Standards**
- Language idioms and best practices adherence
- Code clarity, readability, and maintainability
- Naming conventions (types, functions, variables, packages)
- Error handling patterns and robustness
- Documentation quality (comments, source code formats like godoc, javadoc, etc, depending on the language), following standards
- Test coverage and testing patterns

### **Architecture & Design**
- Overall architecture patterns and design principles
- Separation of concerns and modularity
- Interface design and abstractions
- Dependency management and coupling
- Package structure and organization
- Design patterns usage (appropriate/inappropriate)

### **Production Readiness**
- Error handling and recovery mechanisms
- Logging and observability considerations
- Configuration management
- Language-dependent resource management (threads, goroutines, channels, memory, operating system resources, etc.)
- Concurrency safety and race condition risks
- Performance considerations and bottlenecks

### **Specific Analysis, if applicable**
- Effective use of concurrency primitives
- Usage patterns and best practices
- Interface usage and composition
- Memory allocation patterns
- Garbage collection considerations
- Standard library usage vs external dependencies

### **Security & Reliability**
- Input validation and sanitization
- Potential security vulnerabilities
- Resource leaks (goroutines, file handles, connections)
- Panic/failure prevention and recovery
- Data race detection needs

### **Framework & Dependencies**
- Third-party dependencies evaluation
- Framework choices appropriateness
- Custom utilities vs standard library usage
- Testing frameworks and tools assessment

## Deliverables

Provide a structured report with:

### **Executive Summary**
- Overall code health score/rating
- Top 3 strengths identified
- Top 3 areas requiring attention

### **Detailed Findings**
For each assessment area, provide:
- Current state analysis
- Specific issues found (with line numbers when applicable)
- Code examples demonstrating problems
- Severity rating (Critical/High/Medium/Low)

### **Actionable Recommendations**

**Immediate Improvements** (can be implemented quickly):
- Specific code changes needed
- Quick wins for code quality

**Long-term Architectural Enhancements**:
- Structural improvements for maintainability
- Scalability considerations

**Performance Optimizations**:
- Bottleneck identification and solutions
- Memory and CPU efficiency improvements

**Production Deployment Readiness**:
- Missing production safeguards
- Monitoring and logging enhancements
- Configuration and environment considerations

**Code Organization Improvements**:
- Package restructuring suggestions
- Separation of concerns enhancements

### **Implementation Guidance**
- Priority order for addressing issues
- Estimated effort for each recommendation
- Potential risks of proposed changes
- Testing strategies for validation

**Format all findings with specific examples, code snippets, and concrete next steps where applicable.**