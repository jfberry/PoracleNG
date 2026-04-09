const client = require('prom-client')

client.collectDefaultMetrics()

// Delivery metrics are now in the Go processor (poracle_delivery_*).
// The alerter only exposes default process metrics.

module.exports = {
	client,
}
