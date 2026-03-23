# Deploying Wilayah-API to Coolify

This guide walks through deploying the Indonesian Regions Fuzzy Search API to a Coolify server.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Architecture Overview](#architecture-overview)
- [Database Setup](#database-setup)
- [Deployment Steps](#deployment-steps)
- [Environment Variables](#environment-variables)
- [Health Checks](#health-checks)
- [Data Ingestion](#data-ingestion)
- [Monitoring & Logs](#monitoring--logs)
- [Troubleshooting](#troubleshooting)

## Prerequisites

- **Coolify Server**: Version 4.x or later with Docker runtime available
- **Git Access**: Ability to push/pull from GitHub (or use public repository)
- **Storage**: ~2GB for PostgreSQL data and backups
- **Memory**: Minimum 1GB RAM (2GB recommended for production)
- **Ports**: 8000 (API), 5432 (PostgreSQL, internal only)

## Architecture Overview

The deployment consists of two services:

```
┌─────────────────────────────────────┐
│         Coolify Server              │
├─────────────────────────────────────┤
│                                     │
│  ┌─────────────────────────────┐   │
│  │   wilayah-api (Fiber+Go)    │   │
│  │   Port: 8000                │   │
│  │   DuckDB or PostgreSQL      │   │
│  └──────────┬────────────────┬─┘   │
│             │                │     │
│             │      (depends_on)   │
│             │                │     │
│  ┌──────────▼────────────────▼─┐  │
│  │   PostgreSQL 16 + pgvector  │  │
│  │   Port: 5432 (internal)     │  │
│  │   Volume: postgres_data     │  │
│  └─────────────────────────────┘  │
│                                     │
└─────────────────────────────────────┘
```

**Recommended**: PostgreSQL backend for production use (better FTS, pgvector support, persistence).

## Database Setup

### Option A: PostgreSQL (Recommended for Production)

PostgreSQL is stateful and persists data across restarts.

1. **Create PostgreSQL Service in Coolify**:
   - Go to **Services** → **Add Service** → **PostgreSQL**
   - Name: `wilayah-db`
   - Version: `16` (with pgvector)
   - Root Password: Generate strong password (save for later)
   - Database Name: `wilayah_indonesia`
   - Username: `wilayah_user`
   - Password: Generate strong password (save for later)

2. **Configure Volumes**:
   - Mount path: `/var/lib/postgresql/data`
   - Size: At least 1GB

3. **Apply Migrations**:
   - After PostgreSQL starts, run migrations via the API container setup (see below)
   - Alternatively, exec into the container:
     ```bash
     docker exec wilayah-db psql -U wilayah_user -d wilayah_indonesia -f /docker-entrypoint-initdb.d/001_enable_extensions.sql
     ```

### Option B: DuckDB (Development/Testing Only)

DuckDB is embedded and loses data on restart. Use only for dev/demo purposes.

- Set `DB_TYPE=duckdb`
- Set `DB_PATH=/app/data/regions.duckdb` (in-memory or persistent path)
- Data will be lost if the container restarts

## Deployment Steps

### 1. Create Source Repository Connection

In Coolify:

1. Go to **Settings** → **Git Sources** (if needed, connect your Git provider)
2. Add your repository:
   - Repository URL: `https://github.com/ilmimris/wilayah-indonesia.git`
   - Access Type: Public (or configure SSH keys if private)

### 2. Create API Application

1. Go to **Services** → **Add Service** → **Docker Compose**
2. **Name**: `wilayah-api`
3. **Source**: 
   - Select Git repository (or paste Dockerfile URL)
   - Branch: `main`
   - Dockerfile path: `Dockerfile`
   - Compose file: `docker-compose.prod.yml`

4. **Build Configuration**:
   - Build Args:
     ```
     DB_TYPE=postgres
     ```

### 3. Configure Environment Variables

In the Coolify service editor, add these environment variables:

```
DB_TYPE=postgres
DATABASE_URL=postgres://wilayah_user:YOUR_POSTGRES_PASSWORD@wilayah-db:5432/wilayah_indonesia?sslmode=disable
PORT=8000
```

**Critical**: Replace `YOUR_POSTGRES_PASSWORD` with the actual password set in PostgreSQL service.

### 4. Configure Resource Limits

Set reasonable limits to prevent resource exhaustion:

- **Memory Limit**: 512MB (API) + 768MB (PostgreSQL)
- **CPU Limit**: 1 core
- **Storage**: 2GB for database

### 5. Set Up Networking

Ensure services can communicate:

- Coolify typically auto-creates an internal network
- Service discovery: Use service name `wilayah-db` for PostgreSQL hostname
- Expose only port `8000` publicly; keep PostgreSQL internal

### 6. Deploy

1. Click **Deploy** in Coolify
2. Monitor logs in real-time
3. Wait for both services to reach "Running" state
4. Verify health checks pass (green indicators)

## Environment Variables

### Required (PostgreSQL Mode)

| Variable | Example | Description |
|----------|---------|-------------|
| `DB_TYPE` | `postgres` | Database backend type |
| `DATABASE_URL` | `postgres://user:pass@host:5432/db?sslmode=disable` | PostgreSQL connection string |
| `PORT` | `8000` | HTTP server listen port |

### Optional

| Variable | Default | Description |
|----------|---------|-------------|
| `DB_PATH` | `md:regions` | DuckDB path (only if `DB_TYPE=duckdb`) |
| `LOG_LEVEL` | `info` | Logging level: `debug`, `info`, `warn`, `error` |

### Connection String Format

```
postgres://USERNAME:PASSWORD@HOST:PORT/DATABASE?sslmode=SSLMODE
```

- `USERNAME`: Database user (e.g., `wilayah_user`)
- `PASSWORD`: Database password
- `HOST`: PostgreSQL service hostname (e.g., `wilayah-db`)
- `PORT`: PostgreSQL port (default: `5432`)
- `DATABASE`: Database name (e.g., `wilayah_indonesia`)
- `sslmode`: `disable` (for internal Docker network), `require` (for external)

## Health Checks

### API Health Endpoint

The API exposes a health check at `/healthz`:

```bash
curl http://your-coolify-server:8000/healthz
```

Expected response: `HTTP 200 OK`

### Verify in Coolify Dashboard

- **Service Status**: Should show "Running" (green)
- **Health Check Logs**: View in service detail panel
- **Port Accessibility**: Test externally if port 8000 is exposed

### Manual Testing

Once deployed, test the API:

```bash
# Search for a region
curl "http://your-coolify-server:8000/v1/search?q=bandung"

# Search by province
curl "http://your-coolify-server:8000/v1/search/province?q=Jawa%20Barat"

# Search by district
curl "http://your-coolify-server:8000/v1/search/district?q=Bandung"
```

## Data Ingestion

### Automatic (First Deployment)

The Docker build process includes data preparation:

1. Downloads SQL dumps (wilayah.sql, wilayah_kodepos.sql, bps_wilayah.sql)
2. Runs ingestor to populate PostgreSQL with indexes

If migrations don't run automatically, manually trigger:

### Manual Data Ingestion

1. Access the container shell in Coolify
2. Run the ingestor:
   ```bash
   /app/regions-ingestor
   ```

3. Or use Docker Compose:
   ```bash
   docker-compose -f docker-compose.prod.yml exec api /app/regions-ingestor
   ```

### Verify Data Population

Query the database:

```bash
docker-compose -f docker-compose.prod.yml exec postgres psql -U wilayah_user -d wilayah_indonesia -c "SELECT COUNT(*) FROM regions;"
```

Expected: Non-zero row count (e.g., 80,000+)

## Monitoring & Logs

### View Logs in Coolify

1. Go to **Services** → **wilayah-api**
2. Click **Logs** tab
3. Filter by service: `api` or `postgres`
4. Tail logs in real-time

### Common Log Patterns

**Successful startup**:
```
2026-03-23T10:15:30Z INFO Server starting port=8000 db_type=postgres
```

**Database connection issue**:
```
2026-03-23T10:15:25Z ERROR Failed to bootstrap HTTP application error="connection refused"
```

**Data ingestion**:
```
2026-03-23T10:16:00Z INFO Ingestion complete rows_loaded=81504 duration_ms=4200
```

### Enable Debug Logging

Set environment variable (requires redeploy):
```
LOG_LEVEL=debug
```

## Troubleshooting

### Service Won't Start

**Symptom**: API container exits immediately or shows "Unhealthy"

**Diagnosis**:
1. Check logs for error messages
2. Verify environment variables are set correctly
3. Ensure PostgreSQL service is running and healthy

**Fix**:
```bash
# Restart services in dependency order
docker-compose -f docker-compose.prod.yml down
docker-compose -f docker-compose.prod.yml up -d postgres
sleep 10  # Wait for PostgreSQL to be ready
docker-compose -f docker-compose.prod.yml up -d api
```

### Database Connection Failures

**Symptom**: `ERROR Failed to bootstrap HTTP application error="connection refused"`

**Diagnosis**:
1. Verify PostgreSQL service is running: `docker ps | grep postgres`
2. Check `DATABASE_URL` matches actual PostgreSQL configuration
3. Confirm hostname resolves (use service name, not IP)

**Fix**:
```bash
# Test connectivity from API container
docker-compose -f docker-compose.prod.yml exec api psql -U wilayah_user -h wilayah-db -d wilayah_indonesia -c "\d"
```

### High Memory Usage

**Symptom**: PostgreSQL or API consuming >80% of available RAM

**Diagnosis**:
1. Check row count: `SELECT COUNT(*) FROM regions;`
2. Monitor query patterns in logs
3. Verify indexes are built: `\d+ regions` in PostgreSQL

**Fix**:
- Increase memory limits in Coolify
- Optimize slow queries (check logs for duration)
- Scale PostgreSQL separately if needed

### Search Results Are Empty

**Symptom**: `GET /v1/search?q=bandung` returns empty array

**Diagnosis**:
1. Verify data was ingested: `SELECT COUNT(*) FROM regions;`
2. Check FTS indexes exist: `SELECT * FROM information_schema.tables WHERE table_name LIKE 'fts%';`
3. Test simple query: `SELECT * FROM regions LIMIT 1;`

**Fix**:
1. Re-run ingestor:
   ```bash
   docker-compose -f docker-compose.prod.yml exec api /app/regions-ingestor
   ```
2. Verify SQL dumps are accessible and not corrupted
3. Check container logs for ingestion errors

### Slow Queries

**Symptom**: API responses take >1 second for simple searches

**Diagnosis**:
1. Check if FTS indexes are built
2. Monitor PostgreSQL performance logs
3. Verify no other heavy processes running

**Fix**:
```bash
# In PostgreSQL
EXPLAIN ANALYZE SELECT * FROM regions WHERE full_text @@ plainto_tsquery('english', 'bandung') LIMIT 10;
```

### Port Already in Use

**Symptom**: Deployment fails with "port 8000 already in use"

**Fix**:
1. Change port in environment: `PORT=8001`
2. Or kill existing process: `lsof -ti :8000 | xargs kill -9`
3. Ensure Coolify firewall rules allow the port

## Backup & Recovery

### PostgreSQL Backups

Enable automated backups in Coolify:

1. Go to PostgreSQL service settings
2. Enable **Backups**
3. Set frequency: Daily or weekly
4. Store backups in persistent volume or external storage

### Manual Backup

```bash
docker-compose -f docker-compose.prod.yml exec postgres pg_dump -U wilayah_user -d wilayah_indonesia > wilayah_backup.sql
```

### Restore from Backup

```bash
docker-compose -f docker-compose.prod.yml exec -T postgres psql -U wilayah_user -d wilayah_indonesia < wilayah_backup.sql
```

## Scaling Considerations

### Single Machine (Current Setup)

- Suitable for: Development, testing, small production workloads (<10 req/s)
- Cost: Low
- Limitations: Single point of failure, limited scalability

### Future Scaling

For high-traffic scenarios:

1. **Separate Database Server**: Host PostgreSQL on dedicated machine
2. **Load Balancer**: Use reverse proxy (nginx) to distribute API requests
3. **Read Replicas**: Set up PostgreSQL read replicas for read-heavy workloads
4. **Caching**: Add Redis layer for frequently accessed regions

## Support & Updates

### Check for Updates

```bash
git pull origin main
docker-compose -f docker-compose.prod.yml build --no-cache
docker-compose -f docker-compose.prod.yml up -d
```

### Update Data Periodically

Run ingestor monthly to refresh region data:

```bash
# Schedule via Coolify cron or manual trigger
/app/regions-ingestor
```

## Security Notes

- **Do not expose PostgreSQL port** to the internet (use internal networking only)
- **Use strong passwords** for database credentials
- **Enable SSL/TLS** if accessing API over untrusted networks
- **Restrict access** to health checks and admin endpoints as needed
- **Rotate credentials** periodically and store securely (use Coolify secrets)

---

For more information, see [README.md](README.md) and [.github/copilot-instructions.md](.github/copilot-instructions.md).
