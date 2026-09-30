package db

import (
	"bufio"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// Sale representa los datos de negocio de una venta simulada
type Sale struct {
	Cliente        string
	Producto       string
	Cantidad       int
	PrecioUnitario float64
	Total          float64
	MetodoPago     string
	FechaHora      time.Time
}

var DB *sql.DB

// loadEnvFile lee el archivo .env si existe en el directorio de trabajo y carga sus variables
func loadEnvFile(path string) {
	if path == "" {
		path = ".env"
	}

	file, err := os.Open(path)
	if err != nil {
		// Si no existe el archivo .env, no es error fatal
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			// Solo establecer si no estaba ya definida en el entorno
			if os.Getenv(key) == "" {
				_ = os.Setenv(key, val)
			}
		}
	}
}

// buildConnStr genera la cadena de conexión cargando primero el archivo .env
func buildConnStr(custom string) string {
	if custom != "" {
		return custom
	}

	// Cargar automáticamente el archivo .env si existe
	loadEnvFile(".env")

	host := os.Getenv("DB_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("DB_PORT")
	if port == "" {
		port = "5432"
	}
	user := os.Getenv("DB_USER")
	if user == "" {
		user = "postgres"
	}
	pass := os.Getenv("DB_PASSWORD")
	dbname := os.Getenv("DB_NAME")
	if dbname == "" {
		dbname = "loadbalancer_db"
	}
	sslmode := os.Getenv("DB_SSLMODE")
	if sslmode == "" {
		sslmode = "disable"
	}

	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, pass, dbname, sslmode)
}

// Connect inicializa la conexión con PostgreSQL y asegura la creación de tablas
func Connect(connStr string) (*sql.DB, error) {
	finalConnStr := buildConnStr(connStr)

	database, err := sql.Open("postgres", finalConnStr)
	if err != nil {
		return nil, fmt.Errorf("error abriendo conexión a postgres: %w", err)
	}

	database.SetMaxOpenConns(25)
	database.SetMaxIdleConns(10)
	database.SetConnMaxLifetime(5 * time.Minute)

	if err := database.Ping(); err != nil {
		return nil, fmt.Errorf("error al hacer ping a postgres: %w", err)
	}

	DB = database

	// Crear las tablas automáticamente si no existen
	if err := autoMigrate(database); err != nil {
		log.Printf("[DB Advertencia] Error en autoMigrate: %v", err)
	}

	log.Println("[DB] Conexión a PostgreSQL establecida correctamente desde .env / configuración")
	return DB, nil
}

// autoMigrate crea las tablas e índices necesarios si no existen.
// servidor_id / servidor_asignado_id son VARCHAR porque en Kubernetes la
// identidad de un backend es el nombre efímero de su Pod (ej.
// "backend-7f9c8d6b5-x2kpq"), no un entero fijo como en proyecto12.
func autoMigrate(database *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS recepcion_transacciones (
		id SERIAL PRIMARY KEY,
		request_id INT NOT NULL,
		origen_emisor VARCHAR(100) NOT NULL DEFAULT 'Generador-HTTP',
		balanceador_id VARCHAR(50) NOT NULL DEFAULT 'k8s-service',
		servidor_asignado_id VARCHAR(253) NOT NULL,
		estado VARCHAR(50) NOT NULL DEFAULT 'DESPACHADO',
		fecha_recepcion TIMESTAMP WITH TIME ZONE DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS ventas (
		id SERIAL PRIMARY KEY,
		request_id INT NOT NULL,
		servidor_id VARCHAR(253) NOT NULL,
		cliente VARCHAR(100) NOT NULL,
		producto VARCHAR(100) NOT NULL,
		cantidad INT NOT NULL,
		precio_unitario NUMERIC(10, 2) NOT NULL,
		total NUMERIC(10, 2) NOT NULL,
		metodo_pago VARCHAR(50) NOT NULL,
		fecha_hora TIMESTAMP WITH TIME ZONE DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_recepcion_req ON recepcion_transacciones(request_id);
	CREATE INDEX IF NOT EXISTS idx_ventas_req ON ventas(request_id);
	CREATE INDEX IF NOT EXISTS idx_ventas_servidor ON ventas(servidor_id);
	`
	_, err := database.Exec(schema)
	return err
}

// LogReception registra el evento de recepción en recepcion_transacciones.
// Ni un Service de Kubernetes ni (antes) NGINX pueden escribir a Postgres,
// así que el generador lo hace al ver qué Pod respondió cada solicitud.
func LogReception(requestID int, origen string, lbID string, serverID string) error {
	if DB == nil {
		return nil
	}

	query := `
		INSERT INTO recepcion_transacciones (request_id, origen_emisor, balanceador_id, servidor_asignado_id, estado, fecha_recepcion)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := DB.Exec(query, requestID, origen, lbID, serverID, "DESPACHADO", time.Now())
	if err != nil {
		log.Printf("[DB Error] Error registrando recepción req %d: %v", requestID, err)
		return err
	}
	return nil
}

// InsertSale inserta los datos de la venta en la tabla ventas
func InsertSale(requestID int, serverID string, s Sale) error {
	if DB == nil {
		return nil
	}

	query := `
		INSERT INTO ventas (request_id, servidor_id, cliente, producto, cantidad, precio_unitario, total, metodo_pago, fecha_hora)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := DB.Exec(query, requestID, serverID, s.Cliente, s.Producto, s.Cantidad, s.PrecioUnitario, s.Total, s.MetodoPago, s.FechaHora)
	if err != nil {
		log.Printf("[DB Error] Error insertando venta req %d: %v", requestID, err)
		return err
	}
	return nil
}

// GetCounts retorna el conteo real de filas en ambas tablas
func GetCounts() (int, int) {
	if DB == nil {
		return 0, 0
	}
	var countRecepcion, countVentas int
	_ = DB.QueryRow("SELECT count(*) FROM recepcion_transacciones").Scan(&countRecepcion)
	_ = DB.QueryRow("SELECT count(*) FROM ventas").Scan(&countVentas)
	return countRecepcion, countVentas
}

// ServedByServer retorna cuántas ventas ha procesado un Pod específico
func ServedByServer(serverID string) int {
	if DB == nil {
		return 0
	}
	var count int
	_ = DB.QueryRow("SELECT count(*) FROM ventas WHERE servidor_id = $1", serverID).Scan(&count)
	return count
}
