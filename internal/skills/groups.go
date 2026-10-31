package skills

import "sort"

// Origin marks whether a group ships with the harness or is curated
// from external sources (Section 12.5): integrated groups are
// distributed and versioned with the harness because Excalibur
// itself depends on them; curated groups only become active after
// format validation (12.8) and evaluation with the Section 23
// suites.
type Origin string

// Group origins.
const (
	OriginIntegrated Origin = "integrated"
	OriginCurated    Origin = "curated"
)

// Group is one specialized skill group (Section 12.3). The ID is in
// English; NameES/NameEN and DescES/DescEN are the visible names
// and descriptions in Spanish and English.
type Group struct {
	ID     string
	NameEN string
	NameES string
	DescEN string
	DescES string
	Origin Origin
}

// Groups lists every group of the Section 12.5 catalog in stable
// order.
func Groups() []Group {
	return []Group{
		{ID: "planning", NameEN: "Planning", NameES: "Planificación", Origin: OriginIntegrated,
			DescEN: "Define requirements and design before implementing.",
			DescES: "Definir requisitos y diseño antes de implementar."},
		{ID: "verification", NameEN: "Verification", NameES: "Finalización y verificación", Origin: OriginIntegrated,
			DescEN: "Check the state of the work before calling it done.",
			DescES: "Comprobar el estado del trabajo antes de darlo por terminado."},
		{ID: "communication", NameEN: "Communication", NameES: "Comunicación y retroalimentación", Origin: OriginIntegrated,
			DescEN: "Give useful, grounded answers to the user.",
			DescES: "Dar respuestas útiles y fundamentadas al usuario."},
		{ID: "delegation", NameEN: "Delegation", NameES: "Subagentes", Origin: OriginIntegrated,
			DescEN: "Delegate and coordinate work between specialized agents.",
			DescES: "Delegar y coordinar tareas entre agentes especializados."},
		{ID: "memory", NameEN: "Memory", NameES: "Memoria y relaciones", Origin: OriginIntegrated,
			DescEN: "Organize and recover the context of the user and their interactions.",
			DescES: "Organizar y recuperar el contexto del usuario y de sus interacciones."},
		{ID: "debugging", NameEN: "Debugging", NameES: "Depuración", Origin: OriginIntegrated,
			DescEN: "Investigate errors and find their causes before fixing them.",
			DescES: "Investigar errores y determinar sus causas antes de corregirlos."},
		{ID: "testing", NameEN: "Testing", NameES: "Pruebas", Origin: OriginIntegrated,
			DescEN: "Drive implementation through tests of the expected behavior.",
			DescES: "Guiar la implementación mediante pruebas del comportamiento esperado."},
		{ID: "software-engineering", NameEN: "Software engineering", NameES: "Ingeniería de software", Origin: OriginCurated,
			DescEN: "Analyze code and work with development and deployment platforms.",
			DescES: "Analizar código y trabajar con plataformas de desarrollo y despliegue."},
		{ID: "design", NameEN: "Interface design", NameES: "Diseño de interfaces", Origin: OriginCurated,
			DescEN: "Apply good design practices to web interfaces, apps, presentations and images.",
			DescES: "Aplicar buenas prácticas de diseño de interfaces web, aplicaciones, presentaciones e imágenes."},
		{ID: "security", NameEN: "Security", NameES: "Seguridad", Origin: OriginCurated,
			DescEN: "Audit code and systems to find and verify vulnerabilities.",
			DescES: "Auditar código y sistemas para detectar y verificar vulnerabilidades."},
		{ID: "google-cloud", NameEN: "Google Cloud", NameES: "Google Cloud", Origin: OriginCurated,
			DescEN: "Work with Google products and technologies.",
			DescES: "Trabajar con productos y tecnologías de Google."},
		{ID: "personal-assistant", NameEN: "Personal assistant", NameES: "Asistente personal y productividad", Origin: OriginCurated,
			DescEN: "Handle the user's communications, calendar and errands with their authorization.",
			DescES: "Gestionar comunicaciones, agenda y trámites del usuario con su autorización."},
		{ID: "research", NameEN: "Research", NameES: "Investigación y análisis", Origin: OriginCurated,
			DescEN: "Research, contrast, monitor and analyze information.",
			DescES: "Investigar, contrastar, monitorizar y analizar información."},
		{ID: "browser", NameEN: "Browser and sources", NameES: "Navegador y fuentes", Origin: OriginCurated,
			DescEN: "Operate the browser and consult public sources.",
			DescES: "Operar el navegador y consultar fuentes públicas."},
		{ID: "documents", NameEN: "Documents", NameES: "Documentos y formatos", Origin: OriginCurated,
			DescEN: "Work with documents, spreadsheets, presentations, PDF and office suites.",
			DescES: "Trabajar con documentos, hojas de cálculo, presentaciones, PDF y la suite Office."},
		{ID: "diagrams", NameEN: "Diagrams and visuals", NameES: "Diagramas, gráficas e imágenes", Origin: OriginCurated,
			DescEN: "Create diagrams, charts and visual artifacts of every kind.",
			DescES: "Crear la mayor variedad posible de diagramas y artefactos visuales."},
		{ID: "automation", NameEN: "Automation", NameES: "Automatización", Origin: OriginCurated,
			DescEN: "Create durable automations and triggers.",
			DescES: "Crear automatizaciones durables y disparadores."},
		{ID: "creative-tools", NameEN: "Creative tools", NameES: "Aplicaciones creativas y técnicas", Origin: OriginCurated,
			DescEN: "Drive apps with their own interface through their tools.",
			DescES: "Manejar las aplicaciones con interfaz propia mediante sus herramientas."},
		{ID: "science", NameEN: "Science", NameES: "Investigación científica", Origin: OriginCurated,
			DescEN: "Support research across scientific fields.",
			DescES: "Apoyar investigaciones en distintos campos."},
		{ID: "marketing", NameEN: "Marketing", NameES: "Marketing", Origin: OriginCurated,
			DescEN: "Plan and run marketing work for the product, its audience and its positioning.",
			DescES: "Planificar y ejecutar tareas de marketing según el producto, su público y su posicionamiento."},
		{ID: "compliance", NameEN: "Compliance", NameES: "Cumplimiento normativo", Origin: OriginCurated,
			DescEN: "Check legal and contractual obligations of the user's work; not professional advice.",
			DescES: "Comprobar obligaciones legales y contractuales del trabajo del usuario, sin sustituir el asesoramiento profesional."},
		{ID: "finance", NameEN: "Finance", NameES: "Finanzas", Origin: OriginCurated,
			DescEN: "Organize and analyze the user's finances; not professional advice.",
			DescES: "Organizar y analizar las finanzas del usuario, sin sustituir el asesoramiento profesional."},
		{ID: "terminal", NameEN: "Terminal", NameES: "Terminal y shell", Origin: OriginCurated,
			DescEN: "Use the Linux shell and core utilities well: pipelines, scripting, files, processes.",
			DescES: "Usar bien el shell de Linux y las utilidades básicas: tuberías, scripts, ficheros, procesos."},
		{ID: "packaging", NameEN: "Packaging", NameES: "Empaquetado y dependencias", Origin: OriginCurated,
			DescEN: "Install and pin dependencies with system and language package managers, including offline installs.",
			DescES: "Instalar y fijar dependencias con gestores de paquetes del sistema y de cada lenguaje, incluso sin red."},
		{ID: "build", NameEN: "Build systems", NameES: "Sistemas de compilación", Origin: OriginCurated,
			DescEN: "Configure and run make, CMake and autotools projects with gcc/clang toolchains.",
			DescES: "Configurar y compilar proyectos make, CMake y autotools con toolchains gcc/clang."},
		{ID: "languages", NameEN: "Languages", NameES: "Lenguajes de programación", Origin: OriginCurated,
			DescEN: "Practical notes for reading, running and fixing code in many programming languages.",
			DescES: "Notas prácticas para leer, ejecutar y corregir código en muchos lenguajes de programación."},
		{ID: "networking", NameEN: "Networking", NameES: "Redes y servicios", Origin: OriginCurated,
			DescEN: "Diagnose networks and run services: DNS, TLS, HTTP servers, SSH and systemd units.",
			DescES: "Diagnosticar redes y operar servicios: DNS, TLS, servidores HTTP, SSH y unidades systemd."},
		{ID: "containers", NameEN: "Containers", NameES: "Contenedores y virtualización", Origin: OriginCurated,
			DescEN: "Run workloads in Docker containers and QEMU virtual machines.",
			DescES: "Ejecutar cargas en contenedores Docker y máquinas virtuales QEMU."},
		{ID: "databases", NameEN: "Databases", NameES: "Bases de datos", Origin: OriginCurated,
			DescEN: "Use SQLite, Postgres and Redis from CLIs and scripts.",
			DescES: "Usar SQLite, Postgres y Redis desde la línea de comandos y scripts."},
		{ID: "data", NameEN: "Data processing", NameES: "Procesamiento de datos", Origin: OriginCurated,
			DescEN: "Clean, slice and convert CSV, JSON and Parquet with jq, awk and columnar tools.",
			DescES: "Limpiar, filtrar y convertir CSV, JSON y Parquet con jq, awk y herramientas columnares."},
		{ID: "ml", NameEN: "ML engineering", NameES: "Ingeniería de ML", Origin: OriginCurated,
			DescEN: "Load, run and serve machine-learning models with PyTorch, Hugging Face and ONNX.",
			DescES: "Cargar, ejecutar y servir modelos de ML con PyTorch, Hugging Face y ONNX."},
		{ID: "media", NameEN: "Media", NameES: "Audio, vídeo e imágenes", Origin: OriginCurated,
			DescEN: "Process audio, video and images with ffmpeg and command-line tools.",
			DescES: "Procesar audio, vídeo e imágenes con ffmpeg y herramientas de línea de comandos."},
	}
}

// GroupByID returns the group with id, or false when unknown.
func GroupByID(id string) (Group, bool) {
	for _, g := range Groups() {
		if g.ID == id {
			return g, true
		}
	}
	return Group{}, false
}

// GroupIDs returns the sorted group identifiers.
func GroupIDs() []string {
	gs := Groups()
	out := make([]string, 0, len(gs))
	for _, g := range gs {
		out = append(out, g.ID)
	}
	sort.Strings(out)
	return out
}
