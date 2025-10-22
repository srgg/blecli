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
#define BLIM_LOG_LEVEL_NONE  0
#define BLIM_LOG_LEVEL_ERROR 1
#define BLIM_LOG_LEVEL_WARN  2
#define BLIM_LOG_LEVEL_INFO  3
#define BLIM_LOG_LEVEL_DEBUG 4

// Default log level (can be overridden by build flags)
#ifndef BLIM_LOG_LEVEL
  #ifdef BLIM_DISABLE_LOGGING
    #define BLIM_LOG_LEVEL BLIM_LOG_LEVEL_NONE
  #else
    #define BLIM_LOG_LEVEL BLIM_LOG_LEVEL_INFO  // Default: INFO and above
  #endif
#endif

// Logging macros (compile out completely if disabled)
#if BLIM_LOG_LEVEL >= BLIM_LOG_LEVEL_ERROR
  #define BLIM_LOG_ERROR(...) Serial.printf("❌ " __VA_ARGS__)
#else
  #define BLIM_LOG_ERROR(...) ((void)0)
#endif

#if BLIM_LOG_LEVEL >= BLIM_LOG_LEVEL_WARN
  #define BLIM_LOG_WARN(...) Serial.printf("⚠️  " __VA_ARGS__)
#else
  #define BLIM_LOG_WARN(...) ((void)0)
#endif

#if BLIM_LOG_LEVEL >= BLIM_LOG_LEVEL_INFO
  #define BLIM_LOG_INFO(...) Serial.printf(__VA_ARGS__)
  #define BLIM_LOG_DONE(...) Serial.printf("✅ " __VA_ARGS__)
#else
  #define BLIM_LOG_INFO(...) ((void)0)
  #define BLIM_LOG_DONE(...) ((void)0)
#endif

#if BLIM_LOG_LEVEL >= BLIM_LOG_LEVEL_DEBUG
  #define BLIM_LOG_DEBUG(...) Serial.printf("🔍 " __VA_ARGS__)
#else
  #define BLIM_LOG_DEBUG(...) ((void)0)
#endif

// Special macro for info messages with custom prefix
#if LOG_LEVEL >= _LOG_LEVEL_INFO_
  #define LOG_INFO_RAW(...) Serial.printf(__VA_ARGS__)
#else
  #define LOG_INFO_RAW(...) ((void)0)
#endif

// Simple println wrapper
#if LOG_LEVEL > _LOG_LEVEL_NONE_
  #define LOG_PRINTLN(msg) Serial.println(msg)
#else
  #define LOG_PRINTLN(msg) ((void)0)
#endif

#endif // LOG_H