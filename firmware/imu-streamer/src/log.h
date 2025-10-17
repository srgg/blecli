/*
 * Logging Abstraction for Embedded Systems
 *
 * Provides compile-time configurable logging that can be completely
 * removed from production builds to save code space and improve performance.
 *
 * Usage:
 *   LOG_INFO("System initialized");
 *   LOG_ERROR("Failed: %s", error_msg);
 *   LOG_DEBUG("Value: %d", value);
 *
 * Build Configuration:
 *   -DDISABLE_LOGGING     - Disable all logging (production)
 *   -DLOG_LEVEL=LOG_LEVEL_ERROR - Set minimum log level
 */

#ifndef LOG_H
#define LOG_H

#include <Arduino.h>

// Log levels
#define LOG_LEVEL_NONE  0
#define LOG_LEVEL_ERROR 1
#define LOG_LEVEL_WARN  2
#define LOG_LEVEL_INFO  3
#define LOG_LEVEL_DEBUG 4

// Default log level (can be overridden by build flags)
#ifndef LOG_LEVEL
  #ifdef DISABLE_LOGGING
    #define LOG_LEVEL LOG_LEVEL_NONE
  #else
    #define LOG_LEVEL LOG_LEVEL_INFO  // Default: INFO and above
  #endif
#endif

// Logging macros (compile out completely if disabled)
#if LOG_LEVEL >= LOG_LEVEL_ERROR
  #define LOG_ERROR(...) Serial.printf("❌ " __VA_ARGS__)
#else
  #define LOG_ERROR(...) ((void)0)
#endif

#if LOG_LEVEL >= LOG_LEVEL_WARN
  #define LOG_WARN(...) Serial.printf("⚠️  " __VA_ARGS__)
#else
  #define LOG_WARN(...) ((void)0)
#endif

#if LOG_LEVEL >= LOG_LEVEL_INFO
  #define LOG_INFO(...) Serial.printf("✅ " __VA_ARGS__)
#else
  #define LOG_INFO(...) ((void)0)
#endif

#if LOG_LEVEL >= LOG_LEVEL_DEBUG
  #define LOG_DEBUG(...) Serial.printf("🔍 " __VA_ARGS__)
#else
  #define LOG_DEBUG(...) ((void)0)
#endif

// Special macro for info messages with custom prefix
#if LOG_LEVEL >= LOG_LEVEL_INFO
  #define LOG_INFO_RAW(...) Serial.printf(__VA_ARGS__)
#else
  #define LOG_INFO_RAW(...) ((void)0)
#endif

// Simple println wrapper
#if LOG_LEVEL > LOG_LEVEL_NONE
  #define LOG_PRINTLN(msg) Serial.println(msg)
#else
  #define LOG_PRINTLN(msg) ((void)0)
#endif

#endif // LOG_H