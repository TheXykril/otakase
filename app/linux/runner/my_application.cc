#include "my_application.h"

#include <flutter_linux/flutter_linux.h>
#include <gtk-layer-shell.h>
#ifdef GDK_WINDOWING_X11
#include <gdk/gdkx.h>
#endif

#include "flutter/generated_plugin_registrant.h"
#include "token_store.h"

struct _MyApplication {
  GtkApplication parent_instance;
  char** dart_entrypoint_arguments;
};

G_DEFINE_TYPE(MyApplication, my_application, GTK_TYPE_APPLICATION)

static void launcher_surface_method(FlMethodChannel* channel,
                                    FlMethodCall* call,
                                    gpointer user_data) {
  if (g_strcmp0(fl_method_call_get_name(call), "isLayerSurface") != 0) {
    fl_method_call_respond_not_implemented(call, nullptr);
    return;
  }
  g_autoptr(FlValue) value = fl_value_new_bool(GPOINTER_TO_INT(user_data));
  fl_method_call_respond_success(call, value, nullptr);
}

// Called when first Flutter frame received.
static void first_frame_cb(MyApplication* self, FlView* view) {
  gtk_widget_show(gtk_widget_get_toplevel(GTK_WIDGET(view)));
}

// Implements GApplication::activate.
static void my_application_activate(GApplication* application) {
  MyApplication* self = MY_APPLICATION(application);
  GtkWindow* window =
      GTK_WINDOW(gtk_application_window_new(GTK_APPLICATION(application)));

  const bool launcher =
      g_strv_contains(self->dart_entrypoint_arguments, "--launcher");

  // No title bar: the app draws its own top bar, like on Windows and macOS.
  // The title still names the window in task bars and window switchers.
  gtk_window_set_title(window, launcher ? "Otakase Launcher" : "Otakase");
  gtk_window_set_decorated(window, FALSE);

  // GTK uses the default size for non-resizable windows on the first map.
  // Advertise the popup's size before the compositor places the surface.
  gtk_window_set_default_size(window, launcher ? 680 : 1280,
                              launcher ? 560 : 720);
  if (launcher) {
    // A layer surface is centered without anchors. Hyprland cannot move
    // or resize it with the modifier binds for application windows.
    if (g_getenv("HYPRLAND_INSTANCE_SIGNATURE") != nullptr &&
        gtk_layer_get_protocol_version() >= 4) {
      gtk_layer_init_for_window(window);
      gtk_layer_set_namespace(window, "otakase-launcher");
      gtk_layer_set_layer(window, GTK_LAYER_SHELL_LAYER_OVERLAY);
      gtk_layer_set_keyboard_mode(window, GTK_LAYER_SHELL_KEYBOARD_MODE_ON_DEMAND);
    }
    gtk_window_set_resizable(window, FALSE);
    gtk_window_set_position(window, GTK_WIN_POS_CENTER);
    GdkVisual* visual =
        gdk_screen_get_rgba_visual(gtk_window_get_screen(window));
    if (visual != nullptr) {
      gtk_widget_set_visual(GTK_WIDGET(window), visual);
    }
  }

  g_autoptr(FlDartProject) project = fl_dart_project_new();
  fl_dart_project_set_dart_entrypoint_arguments(
      project, self->dart_entrypoint_arguments);

  FlView* view = fl_view_new(project);
  // Let Flutter draw the launcher's first visible pixels after placement.
  GdkRGBA background_color = {};
  if (!launcher) {
    gdk_rgba_parse(&background_color, "#0e0d0c");
  }
  fl_view_set_background_color(view, &background_color);
  gtk_widget_show(GTK_WIDGET(view));
  gtk_container_add(GTK_CONTAINER(window), GTK_WIDGET(view));

  // Show the window when Flutter renders.
  // Requires the view to be realized so we can start rendering.
  g_signal_connect_swapped(view, "first-frame", G_CALLBACK(first_frame_cb),
                           self);
  gtk_widget_realize(GTK_WIDGET(view));

  fl_register_plugins(FL_PLUGIN_REGISTRY(view));
  token_store_register(view);
  if (launcher) {
    g_autoptr(FlStandardMethodCodec) codec = fl_standard_method_codec_new();
    g_autoptr(FlMethodChannel) channel = fl_method_channel_new(
        fl_engine_get_binary_messenger(fl_view_get_engine(view)),
        "otakase/launcher_surface", FL_METHOD_CODEC(codec));
    fl_method_channel_set_method_call_handler(
        channel, launcher_surface_method,
        GINT_TO_POINTER(gtk_layer_is_layer_window(window)), nullptr);
  }

  gtk_widget_grab_focus(GTK_WIDGET(view));
}

// Implements GApplication::local_command_line.
static gboolean my_application_local_command_line(GApplication* application,
                                                  gchar*** arguments,
                                                  int* exit_status) {
  MyApplication* self = MY_APPLICATION(application);
  // Strip out the first argument as it is the binary name.
  self->dart_entrypoint_arguments = g_strdupv(*arguments + 1);

  g_autoptr(GError) error = nullptr;
  if (!g_application_register(application, nullptr, &error)) {
    g_warning("Failed to register: %s", error->message);
    *exit_status = 1;
    return TRUE;
  }

  g_application_activate(application);
  *exit_status = 0;

  return TRUE;
}

// Implements GApplication::startup.
static void my_application_startup(GApplication* application) {
  // MyApplication* self = MY_APPLICATION(object);

  // Perform any actions required at application startup.

  G_APPLICATION_CLASS(my_application_parent_class)->startup(application);
}

// Implements GApplication::shutdown.
static void my_application_shutdown(GApplication* application) {
  // MyApplication* self = MY_APPLICATION(object);

  // Perform any actions required at application shutdown.

  G_APPLICATION_CLASS(my_application_parent_class)->shutdown(application);
}

// Implements GObject::dispose.
static void my_application_dispose(GObject* object) {
  MyApplication* self = MY_APPLICATION(object);
  g_clear_pointer(&self->dart_entrypoint_arguments, g_strfreev);
  G_OBJECT_CLASS(my_application_parent_class)->dispose(object);
}

static void my_application_class_init(MyApplicationClass* klass) {
  G_APPLICATION_CLASS(klass)->activate = my_application_activate;
  G_APPLICATION_CLASS(klass)->local_command_line =
      my_application_local_command_line;
  G_APPLICATION_CLASS(klass)->startup = my_application_startup;
  G_APPLICATION_CLASS(klass)->shutdown = my_application_shutdown;
  G_OBJECT_CLASS(klass)->dispose = my_application_dispose;
}

static void my_application_init(MyApplication* self) {}

MyApplication* my_application_new() {
  // Set the program name to the application ID, which helps various systems
  // like GTK and desktop environments map this running application to its
  // corresponding .desktop file. This ensures better integration by allowing
  // the application to be recognized beyond its binary name.
  g_set_prgname(APPLICATION_ID);

  return MY_APPLICATION(g_object_new(my_application_get_type(),
                                     "application-id", APPLICATION_ID, "flags",
                                     G_APPLICATION_NON_UNIQUE, nullptr));
}
