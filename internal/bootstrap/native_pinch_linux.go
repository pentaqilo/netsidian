//go:build !server && linux && !gtk3

package bootstrap

/*
#cgo pkg-config: gtk4 webkitgtk-6.0
#include <gtk/gtk.h>
#include <webkit/webkit.h>
#include <math.h>

// WebKitGTK consumes GDK touchpad pinches without emitting DOM gesture events.
// Intercept them before the webview and send an explicit graph-only DOM event.
typedef struct {
    WebKitWebView *view;
    double previous, x, y;
} NetsidianPinch;

static GtkWidget *netsidian_find_webview(GtkWidget *widget) {
    if (WEBKIT_IS_WEB_VIEW(widget)) return widget;
    for (GtkWidget *child = gtk_widget_get_first_child(widget); child; child = gtk_widget_get_next_sibling(child)) {
        GtkWidget *view = netsidian_find_webview(child);
        if (view) return view;
    }
    return NULL;
}

static gboolean netsidian_pinch_event(GtkEventControllerLegacy *controller, GdkEvent *event, gpointer data) {
    if (gdk_event_get_event_type(event) != GDK_TOUCHPAD_PINCH) return FALSE;
    NetsidianPinch *pinch = data;
    GdkTouchpadGesturePhase phase = gdk_touchpad_event_get_gesture_phase(event);
    const char *name = "change";
    double factor = 1;
    if (phase == GDK_TOUCHPAD_GESTURE_PHASE_BEGIN) {
        pinch->previous = 1;
        double x = 0, y = 0;
        gdk_event_get_position(event, &x, &y);
        graphene_point_t from = GRAPHENE_POINT_INIT(x, y), to;
        // GDK coordinates are relative to the native surface, not the DOM.
        GtkWidget *root = GTK_WIDGET(gtk_widget_get_native(GTK_WIDGET(pinch->view)));
        if (!gtk_widget_compute_point(root, GTK_WIDGET(pinch->view), &from, &to)) return TRUE;
        pinch->x = to.x; pinch->y = to.y;
        name = "start";
    } else if (phase == GDK_TOUCHPAD_GESTURE_PHASE_UPDATE) {
        double scale = gdk_touchpad_event_get_pinch_scale(event);
        if (!isfinite(scale) || scale <= 0 || pinch->previous <= 0) return TRUE;
        factor = scale / pinch->previous;
        pinch->previous = scale;
    } else {
        name = "end";
        pinch->previous = 0;
    }
    // JavaScript numbers always use dots, including under a comma-decimal locale.
    char factor_text[G_ASCII_DTOSTR_BUF_SIZE], x_text[G_ASCII_DTOSTR_BUF_SIZE], y_text[G_ASCII_DTOSTR_BUF_SIZE];
    g_ascii_dtostr(factor_text, sizeof factor_text, factor);
    g_ascii_dtostr(x_text, sizeof x_text, pinch->x);
    g_ascii_dtostr(y_text, sizeof y_text, pinch->y);
    char *script = g_strdup_printf(
        "document.dispatchEvent(new CustomEvent('netsidian-native-pinch',{detail:{phase:'%s',factor:%s,clientX:%s,clientY:%s}}));",
        name, factor_text, x_text, y_text);
    webkit_web_view_evaluate_javascript(pinch->view, script, -1, NULL, NULL, NULL, NULL, NULL);
    g_free(script);
    return TRUE; // Suppress WebKit page magnification, including outside graphs.
}

static void netsidian_install_pinch(void *native_window) {
    if (!native_window) return;
    GtkWidget *window = GTK_WIDGET(native_window);
    if (g_object_get_data(G_OBJECT(window), "netsidian-pinch-installed")) return;
    GtkWidget *view = netsidian_find_webview(window);
    if (!view) return;
    NetsidianPinch *pinch = g_new0(NetsidianPinch, 1);
    pinch->view = WEBKIT_WEB_VIEW(view);
    GtkEventController *controller = gtk_event_controller_legacy_new();
    gtk_event_controller_set_propagation_phase(controller, GTK_PHASE_CAPTURE);
    g_object_set_data_full(G_OBJECT(controller), "netsidian-pinch-state", pinch, g_free);
    g_signal_connect(controller, "event", G_CALLBACK(netsidian_pinch_event), pinch);
    gtk_widget_add_controller(window, controller);
    g_object_set_data(G_OBJECT(window), "netsidian-pinch-installed", GINT_TO_POINTER(1));
}
*/
import "C"

import "unsafe"

func installNativeGraphPinch(window unsafe.Pointer) {
	C.netsidian_install_pinch(window)
}
