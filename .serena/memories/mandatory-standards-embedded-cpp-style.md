# MANDATORY: Embedded C/C++ Standards

## Naming Conventions

- **Files:** `snake_case.c/h`
- **Functions/Variables:** `snake_case` (`gpio_init`, `button_state`)
- **Constants/Macros/Registers:** `UPPER_SNAKE_CASE` (`MAX_SIZE`, `GPIOA_ODR`)
- **Classes/Structs:** `PascalCase` (`SensorDriver`)
- **Globals:** `g_` prefix (`g_state`)
- **Static:** `s_` prefix (`s_buffer`)

## Comments
**CRITICAL:** Use doxygen style comments to ensure that documentation can be generated automatically.

### Section Comments

**CRITICAL:** Do not confuse or replace section comments with class, function, or method definitions.
**Format:** `// Section Name` (plain text, no decorative characters)

**DO NOT use:** `====`, `----`, `****`, or any other decorative symbols.

**Use ONLY when:**
- Grouping code sections that are **not clearly separated by structure or naming**, or
- The section spans **more than 10 lines** and has a **distinct functional purpose**.

**Default:** Prefer a **blank line** over a section comment when the grouping is already clear.

## **Critical:** C++ Embedded Code Compliance
All C/C++ code must strictly adhere to:
1. **C++ Core Guidelines** (https://isocpp.github.io/CppCoreGuidelines/CppCoreGuidelines)
2. **Clang-Tidy checks** as configured in the project (including modernize, bugprone, performance, readability, and misc modules)
3. **Embedded best practices** including
     - Deterministic memory usage (avoid dynamic allocations on runtime-critical paths unless justified)
     - Minimal stack usage
     - Efficient CPU and power usage
     - Safe peripheral and register access patterns
     - Board/device-specific coding rules (must comply with provided hardware abstraction layer [HAL] and manufacturer guidelines)
     - General software engineering best practices:
     - Consistent naming conventions
     - Proper error handling
     - Avoidance of undefined behavior
     - Clear and maintainable structure

**Mandatory reinforcement:**
  - Code failing Clang-Tidy checks or violating C++ Core Guidelines will not be merged.
  - Code must pass all board-specific static analysis and code review checks.
  - Automated CI checks must be configured to enforce compliance.

## Memory Management

**CRITICAL:** Static allocation only, no heap.

```cpp
// ✅ CORRECT
static uint8_t rx_buffer[256];
static SensorData readings[10];

// ❌ WRONG
auto* buffer = new uint8_t[256];  // No heap!
std::vector<int> data;            // Dynamic allocation!
```

**MUST:**
- Use static allocation
- Use memory pools if dynamic needed
- Know memory footprint at compile time
- NEVER allocate in ISRs

## Hardware Interaction

```cpp
// ✅ CORRECT: Volatile for registers
volatile uint32_t* const GPIO_ODR = (uint32_t*)0x40020014;

// Bit manipulation
#define GPIO_PIN_5 (1U << 5)
*GPIO_ODR |= GPIO_PIN_5;   // Set
*GPIO_ODR &= ~GPIO_PIN_5;  // Clear

// Struct overlay
struct GPIORegs {
    volatile uint32_t MODER;
    volatile uint32_t ODR;
};
```

**MUST:**
- Use `volatile` for hardware registers
- Document register addresses
- Use bit masks for clarity

## Interrupt Safety

```cpp
// ✅ CORRECT: Keep ISRs minimal
extern "C" void USART1_IRQHandler(void) {
    if (USART1->SR & USART_SR_RXNE) {
        rx_buffer[rx_index++] = USART1->DR;
        data_ready = true;  // Set flag only
    }
}

// Critical section
void critical_section() {
    __disable_irq();
    // Critical code
    __enable_irq();
}
```

**MUST in ISRs:**
- Keep minimal
- Set flags, defer processing
- Use `volatile` for shared data
- NEVER call blocking functions
- NEVER allocate memory

## Error Handling (No Exceptions)

```cpp
// ✅ CORRECT: Error codes
enum class Status : uint8_t {
    OK = 0,
    ERROR_TIMEOUT,
    ERROR_INVALID_PARAM
};

Status uart_transmit(const uint8_t* data, size_t len) {
    if (!data) return Status::ERROR_INVALID_PARAM;
    // Implementation
    return Status::OK;
}

// Check all returns
if (uart_transmit(data, len) != Status::OK) {
    // Handle error
}
```

**MUST:**
- Use error codes or result types
- Check ALL return values
- Use `[[nodiscard]]`

**NEVER:**
- Use exceptions (disabled)
- Ignore return values

## Real-Time Constraints

```cpp
// ✅ CORRECT: Bounded execution
Status process() {
    uint32_t start = get_tick();
    while (!ready) {
        if (get_tick() - start > TIMEOUT_MS) {
            return Status::ERROR_TIMEOUT;
        }
    }
    return Status::OK;
}
```

**MUST:**
- Use timeouts for all operations
- Ensure bounded execution time
- Document timing requirements

**NEVER:**
- Use unbounded loops
- Use blocking without timeout

## Embedded C++ Subset

```cpp
// ✅ Safe subset
class Driver {
    void init();  // No virtual (no vtables)
    
    template<uint8_t Pin>
    void setGPIO();  // Templates OK
    
    static constexpr uint32_t BAUD = 115200;  // constexpr OK
};
```

**MUST:**
- Use templates (no runtime cost)
- Use `constexpr` for compile-time

**NEVER:**
- Use virtual functions (vtable overhead)
- Use RTTI (`dynamic_cast`, `typeid`)
- Use exceptions
- Use dynamic allocation
- Use std containers (unless embedded-friendly)

## Build Configuration

```cmake
# Cross-compile
set(CMAKE_SYSTEM_NAME Generic)
set(CMAKE_C_COMPILER arm-none-eabi-gcc)

add_compile_options(
    -mcpu=cortex-m4
    -mthumb
    -fno-exceptions
    -fno-rtti
    -Os  # Optimize for size
)

add_link_options(
    -Wl,--gc-sections  # Remove unused
    --specs=nano.specs
)
```

**MUST:**
- Disable exceptions/RTTI
- Optimize for size (`-Os`)
- Remove unused code (`-Wl,--gc-sections`)
- Set correct MCU flags

## Code Quality

**MUST:**
- Zero warnings (`-Wall -Wextra -Werror`)
- Static analysis (`cppcheck`)
- MISRA C/C++ (if required)
- Test on target hardware

## Enforcement

These standards are **NON-NEGOTIABLE**.