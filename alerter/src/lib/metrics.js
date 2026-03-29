const client = require('prom-client')

client.collectDefaultMetrics()

// Queue depths (sampled at scrape time)
const discordQueueDepth = new client.Gauge({
	name: 'poracle_alerter_discord_queue_depth',
	help: 'Current discord bot queue depth',
})

const discordWebhookQueueDepth = new client.Gauge({
	name: 'poracle_alerter_discord_webhook_queue_depth',
	help: 'Current discord webhook queue depth',
})

const telegramQueueDepth = new client.Gauge({
	name: 'poracle_alerter_telegram_queue_depth',
	help: 'Current telegram queue depth',
})

// Delivery
const messagesSent = new client.Counter({
	name: 'poracle_alerter_messages_sent_total',
	help: 'Messages successfully delivered',
	labelNames: ['destination_type'],
})

const messagesFailed = new client.Counter({
	name: 'poracle_alerter_messages_failed_total',
	help: 'Messages that failed to deliver',
	labelNames: ['destination_type'],
})

const discordDeliveryDuration = new client.Histogram({
	name: 'poracle_alerter_discord_delivery_seconds',
	help: 'Discord bot delivery time',
	labelNames: ['destination_type'],
	buckets: [0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30],
})

const discordWebhookDeliveryDuration = new client.Histogram({
	name: 'poracle_alerter_discord_webhook_delivery_seconds',
	help: 'Discord webhook delivery time',
	buckets: [0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30],
})

const telegramDeliveryDuration = new client.Histogram({
	name: 'poracle_alerter_telegram_delivery_seconds',
	help: 'Telegram delivery time',
	labelNames: ['destination_type'],
	buckets: [0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30],
})

const discordRateLimits = new client.Counter({
	name: 'poracle_alerter_discord_rate_limits_total',
	help: 'Discord 429 rate limit events',
	labelNames: ['source'],
})

const telegramRateLimits = new client.Counter({
	name: 'poracle_alerter_telegram_rate_limits_total',
	help: 'Telegram 429 rate limit events',
})

module.exports = {
	client,
	discordQueueDepth,
	discordWebhookQueueDepth,
	telegramQueueDepth,
	messagesSent,
	messagesFailed,
	discordDeliveryDuration,
	discordWebhookDeliveryDuration,
	telegramDeliveryDuration,
	discordRateLimits,
	telegramRateLimits,
}
