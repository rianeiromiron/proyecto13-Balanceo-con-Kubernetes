-- Registro de recepción: qué Pod backend asignó Kubernetes a cada solicitud.
-- Lo inserta el generador al leer el header X-Server-Id de la respuesta,
-- porque ni un Service de Kubernetes ni NGINX pueden escribir a Postgres.
--
-- servidor_id / servidor_asignado_id son VARCHAR (no INT como en proyecto12)
-- porque la identidad de un backend acá es el nombre efímero de su Pod
-- (ej. "backend-7f9c8d6b5-x2kpq"), no un entero fijo por contenedor.
CREATE TABLE IF NOT EXISTS recepcion_transacciones (
    id SERIAL PRIMARY KEY,
    request_id INT NOT NULL,
    origen_emisor VARCHAR(100) NOT NULL DEFAULT 'Generador-HTTP',
    balanceador_id VARCHAR(50) NOT NULL DEFAULT 'k8s-service',
    servidor_asignado_id VARCHAR(253) NOT NULL,
    estado VARCHAR(50) NOT NULL DEFAULT 'DESPACHADO',
    fecha_recepcion TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Datos de negocio de la venta simulada, insertados por el Pod que la procesó
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

-- Índices para optimizar consultas en tiempo real
CREATE INDEX IF NOT EXISTS idx_recepcion_req ON recepcion_transacciones(request_id);
CREATE INDEX IF NOT EXISTS idx_ventas_req ON ventas(request_id);
CREATE INDEX IF NOT EXISTS idx_ventas_servidor ON ventas(servidor_id);
