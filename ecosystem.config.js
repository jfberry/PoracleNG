// pm2 ecosystem file for PoracleNG
// Usage: pm2 start ecosystem.config.js
module.exports = {
  apps: [{
    name: 'poracle',
    script: './start.sh',
    kill_timeout: 10000,       // 10s for graceful shutdown (default 1600ms is too short)
    listen_timeout: 30000,     // 30s for startup (processor health check)
    max_restarts: 10,
    restart_delay: 5000,
    autorestart: true,
  }]
}
