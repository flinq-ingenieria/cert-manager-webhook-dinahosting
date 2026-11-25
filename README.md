# cert-manager-webhook-dinahosting

Solver ACME DNS-01 para [cert-manager](https://cert-manager.io) que crea y borra
los registros TXT `_acme-challenge` mediante la
[API de Dinahosting](https://dinahosting.com/api). Permite emitir y renovar
automáticamente certificados **wildcard** para dominios con DNS en Dinahosting.

- `groupName`: `acme.zverp.com` (configurable en el chart)
- `solverName`: `dinahosting`

## Cómo funciona

| Paso cert-manager | Llamada a la API |
|---|---|
| `Present` | `Domain_Zone_GetTypeTXT` (para ser idempotente) y `Domain_Zone_AddTypeTXT(domain, hostname, text)` |
| `CleanUp` | `Domain_Zone_DeleteTypeTXT(domain, hostname, value)`: borra solo el TXT con ese valor |

- `domain` es la zona que resuelve cert-manager (p. ej. `zverp.com`) y `hostname`
  el nombre relativo (p. ej. `_acme-challenge.runboat`).
- Se usa la petición *Simple* documentada: POST `application/x-www-form-urlencoded`
  con `responseType=Json`. Las credenciales van como `AUTH_USER`/`AUTH_PWD` en el
  cuerpo y en `Authorization: Basic` (ambos métodos documentados), nunca en la URL.
- `2302 OBJECT_EXISTS` en el alta y `2303 OBJECT_NOT_EXISTS` en el borrado se tratan
  como éxito (reintentos de cert-manager).
- Al borrar se indica el valor, así que dos validaciones simultáneas sobre el
  mismo nombre (`zverp.com` y `*.zverp.com`) no se pisan.

## Instalación

```bash
# 1. Credenciales de la API de Dinahosting (namespace de recursos de cluster de cert-manager)
kubectl -n cert-manager create secret generic dinahosting-credentials \
  --from-literal=username=<usuario-api> --from-literal=password=<contraseña-api>

# 2. Webhook (usar este nombre de release para que los recursos tengan nombres cortos)
helm upgrade --install cert-manager-webhook-dinahosting \
  deploy/cert-manager-webhook-dinahosting --namespace cert-manager

kubectl get apiservice v1alpha1.acme.zverp.com   # debe estar Available=True

# 3. ClusterIssuer y certificados: ver examples/
```

El webhook solo puede leer los secretos listados en `credentialsSecretNames`
(por defecto `dinahosting-credentials`) del namespace `cert-manager`.

Si el paquete de GHCR es privado, crear un `imagePullSecret` y pasarlo en
`imagePullSecrets`.

## Ejemplos

- [`examples/clusterissuer-zerossl-dns.yaml`](examples/clusterissuer-zerossl-dns.yaml):
  ClusterIssuer ZeroSSL con DNS-01.
- [`examples/certificate-test.yaml`](examples/certificate-test.yaml): certificado de
  prueba en un secreto temporal.
- [`examples/certificates-wildcard.yaml`](examples/certificates-wildcard.yaml):
  `*.zverp.com` + `*.runboat.zverp.com` como certificado por defecto del ingress (TLSStore de Traefik),
  y el wildcard de runboat en su secreto actual.

## Desarrollo

```bash
make test                      # tests unitarios (sin red ni credenciales)
make docker-test               # lo mismo, sin Go instalado

# Test de conformidad de cert-manager contra la API y el DNS reales.
# Crea y borra TXT cert-manager-dns01-tests.<zona>.
cp testdata/dinahosting/secret.yaml.example testdata/dinahosting/secret.yaml  # y rellenar
TEST_ZONE_NAME=zverp.com. make test-conformance   # o make docker-test-conformance

make image TAG=dev
make chart-lint
```

CI (GitHub Actions): tests y lint en cada push/PR; imagen en
`ghcr.io/flinq-ingenieria/cert-manager-webhook-dinahosting` en `main` (tags `main`,
`sha-…`) y en tags `vX.Y.Z` (tag `X.Y.Z`, que es el que usa el chart por defecto).

## Licencia

Apache 2.0. Basado en [cert-manager/webhook-example](https://github.com/cert-manager/webhook-example).
