package daemon

import "testing"

func TestLooksLikeAppBuild(t *testing.T) {
	for _, tc := range []struct {
		text string
		want bool
	}{
		{"Mi cliente aprobó estos TDR. Prográmalo todo.", true},
		{"Build the sales and inventory web system described in the brief", true},
		{"Créame una aplicación web para reservas", true},
		{"Diseña un dashboard de ventas", true},
		{"¿Cuáles fueron mis gastos la semana pasada?", false},
		{"Verifica qué tareas pendientes tengo en el intranet de mi universidad", false},
		{"Hazme una presentación sobre el proyecto", false},
	} {
		if got := looksLikeAppBuild(tc.text); got != tc.want {
			t.Errorf("looksLikeAppBuild(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}
