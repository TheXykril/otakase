#include "token_store.h"

#include <glib/gstdio.h>
#include <libsecret/secret.h>

#include <cerrno>
#include <cstring>

// Sign-in tokens live in the desktop keyring through the Secret Service
// (GNOME Keyring, KWallet, KeePassXC), one item per key. A desktop with no
// Secret Service at all keeps them in private files under the app's data
// folder instead, readable by this user only.
//
// The keyring plugin the app used before stored everything in one item and
// tagged it with a schema name it had already freed, so a later start could
// not find it again and the next write replaced it: sign-ins did not survive
// a restart. "legacy" and "clearLegacy" read and remove that old item once.

namespace {

const SecretSchema kSchema = {
    APPLICATION_ID ".Token",
    SECRET_SCHEMA_NONE,
    {
        {"key", SECRET_SCHEMA_ATTRIBUTE_STRING},
        {nullptr, SECRET_SCHEMA_ATTRIBUTE_STRING},
    }};

// The old item: matched by its account attribute only.
const SecretSchema kLegacySchema = {
    "",
    SECRET_SCHEMA_DONT_MATCH_NAME,
    {
        {"account", SECRET_SCHEMA_ATTRIBUTE_STRING},
        {nullptr, SECRET_SCHEMA_ATTRIBUTE_STRING},
    }};
const char kLegacyAccount[] = APPLICATION_ID ".secureStorage";

enum class Failure { kNone, kUnavailable, kLocked, kOther };

Failure classify(const GError* error) {
  if (error == nullptr) return Failure::kNone;
  if (g_error_matches(error, SECRET_ERROR, SECRET_ERROR_IS_LOCKED) ||
      g_error_matches(error, G_IO_ERROR, G_IO_ERROR_CANCELLED)) {
    return Failure::kLocked;
  }
  // No session bus, or nobody provides org.freedesktop.secrets.
  if (error->domain == G_DBUS_ERROR || error->domain == G_IO_ERROR) {
    return Failure::kUnavailable;
  }
  return Failure::kOther;
}

bool valid_key(const char* key) {
  if (key == nullptr || key[0] == '\0' || std::strlen(key) > 200 ||
      key[0] == '.') {
    return false;
  }
  for (const char* c = key; *c != '\0'; c++) {
    if (!g_ascii_isalnum(*c) && *c != '.' && *c != '_' && *c != '-') {
      return false;
    }
  }
  return true;
}

gchar* fallback_dir() {
  return g_build_filename(g_get_user_data_dir(), APPLICATION_ID, "tokens",
                          nullptr);
}

gchar* fallback_path(const char* key) {
  g_autofree gchar* dir = fallback_dir();
  return g_build_filename(dir, key, nullptr);
}

gchar* fallback_read(const char* key) {
  g_autofree gchar* path = fallback_path(key);
  gchar* value = nullptr;
  if (!g_file_get_contents(path, &value, nullptr, nullptr)) return nullptr;
  return value;
}

bool fallback_write(const char* key, const char* value, GError** error) {
  g_autofree gchar* dir = fallback_dir();
  if (g_mkdir_with_parents(dir, 0700) != 0) {
    g_set_error(error, G_FILE_ERROR, g_file_error_from_errno(errno),
                "Unable to create %s", dir);
    return false;
  }
  g_chmod(dir, 0700);
  g_autofree gchar* path = fallback_path(key);
  return g_file_set_contents_full(path, value, -1,
                                  G_FILE_SET_CONTENTS_CONSISTENT, 0600, error);
}

void fallback_delete(const char* key) {
  g_autofree gchar* path = fallback_path(key);
  g_unlink(path);
}

FlMethodResponse* failure(Failure kind, const GError* error) {
  const char* code = kind == Failure::kLocked        ? "locked"
                     : kind == Failure::kUnavailable ? "unavailable"
                                                     : "failed";
  return FL_METHOD_RESPONSE(fl_method_error_response_new(
      code, error != nullptr ? error->message : code, nullptr));
}

FlMethodResponse* success(FlValue* value) {
  return FL_METHOD_RESPONSE(fl_method_success_response_new(value));
}

FlMethodResponse* read_key(const char* key) {
  g_autoptr(GError) error = nullptr;
  gchar* secret = secret_password_lookup_sync(&kSchema, nullptr, &error, "key",
                                              key, nullptr);
  const Failure kind = classify(error);
  if (kind != Failure::kNone && kind != Failure::kUnavailable) {
    return failure(kind, error);
  }
  if (secret != nullptr) {
    g_autoptr(FlValue) value = fl_value_new_string(secret);
    secret_password_free(secret);
    return success(value);
  }
  g_autofree gchar* stored = fallback_read(key);
  if (stored != nullptr) {
    g_autoptr(FlValue) value = fl_value_new_string(stored);
    return success(value);
  }
  return success(nullptr);
}

FlMethodResponse* write_key(const char* key, const char* value) {
  g_autoptr(GError) error = nullptr;
  g_autofree gchar* label = g_strdup_printf("Otakase sign-in (%s)", key);
  secret_password_store_sync(&kSchema, SECRET_COLLECTION_DEFAULT, label, value,
                             nullptr, &error, "key", key, nullptr);
  const Failure kind = classify(error);
  if (kind == Failure::kNone) {
    // The keyring holds it now; drop any copy kept while it was missing.
    fallback_delete(key);
    return success(nullptr);
  }
  if (kind != Failure::kUnavailable) return failure(kind, error);
  g_autoptr(GError) file_error = nullptr;
  if (!fallback_write(key, value, &file_error)) {
    return failure(Failure::kOther, file_error);
  }
  return success(nullptr);
}

FlMethodResponse* delete_key(const char* key) {
  fallback_delete(key);
  g_autoptr(GError) error = nullptr;
  secret_password_clear_sync(&kSchema, nullptr, &error, "key", key, nullptr);
  const Failure kind = classify(error);
  if (kind != Failure::kNone && kind != Failure::kUnavailable) {
    return failure(kind, error);
  }
  return success(nullptr);
}

FlMethodResponse* read_legacy() {
  g_autoptr(GError) error = nullptr;
  gchar* secret = secret_password_lookup_sync(
      &kLegacySchema, nullptr, &error, "account", kLegacyAccount, nullptr);
  if (secret == nullptr) return success(nullptr);
  g_autoptr(FlValue) value = fl_value_new_string(secret);
  secret_password_free(secret);
  return success(value);
}

FlMethodResponse* clear_legacy() {
  g_autoptr(GError) error = nullptr;
  secret_password_clear_sync(&kLegacySchema, nullptr, &error, "account",
                             kLegacyAccount, nullptr);
  return success(nullptr);
}

const char* string_arg(FlValue* args, const char* name) {
  if (args == nullptr || fl_value_get_type(args) != FL_VALUE_TYPE_MAP) {
    return nullptr;
  }
  FlValue* value = fl_value_lookup_string(args, name);
  if (value == nullptr || fl_value_get_type(value) != FL_VALUE_TYPE_STRING) {
    return nullptr;
  }
  return fl_value_get_string(value);
}

void method_call_cb(FlMethodChannel* channel, FlMethodCall* call,
                    gpointer user_data) {
  const gchar* method = fl_method_call_get_name(call);
  FlValue* args = fl_method_call_get_args(call);
  g_autoptr(FlMethodResponse) response = nullptr;
  if (std::strcmp(method, "legacy") == 0) {
    response = read_legacy();
  } else if (std::strcmp(method, "clearLegacy") == 0) {
    response = clear_legacy();
  } else {
    const char* key = string_arg(args, "key");
    if (std::strcmp(method, "read") != 0 && std::strcmp(method, "write") != 0 &&
        std::strcmp(method, "delete") != 0) {
      response = FL_METHOD_RESPONSE(fl_method_not_implemented_response_new());
    } else if (!valid_key(key)) {
      response = FL_METHOD_RESPONSE(
          fl_method_error_response_new("bad_key", "Invalid key.", nullptr));
    } else if (std::strcmp(method, "read") == 0) {
      response = read_key(key);
    } else if (std::strcmp(method, "delete") == 0) {
      response = delete_key(key);
    } else {
      const char* value = string_arg(args, "value");
      response = value == nullptr ? delete_key(key) : write_key(key, value);
    }
  }
  fl_method_call_respond(call, response, nullptr);
}

}  // namespace

void token_store_register(FlView* view) {
  FlEngine* engine = fl_view_get_engine(view);
  g_autoptr(FlStandardMethodCodec) codec = fl_standard_method_codec_new();
  FlMethodChannel* channel = fl_method_channel_new(
      fl_engine_get_binary_messenger(engine), "otakase/tokens",
      FL_METHOD_CODEC(codec));
  // The channel lives as long as the app's one window.
  fl_method_channel_set_method_call_handler(channel, method_call_cb, nullptr,
                                            nullptr);
}
