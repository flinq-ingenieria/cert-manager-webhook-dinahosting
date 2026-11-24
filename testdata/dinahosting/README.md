# testdata/dinahosting

Manifiestos que el test de conformidad aplica en un namespace temporal de envtest.

- `config.json`: configuración del solver (igual que en el ClusterIssuer).
- `secret.yaml`: **no se versiona**. Copiar `secret.yaml.example` y rellenar con
  las credenciales de la API de Dinahosting.

El archivo `.example` no termina en `.yaml`, por eso el test no lo aplica.
