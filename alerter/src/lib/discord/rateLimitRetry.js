/**
 * Wrap a Discord API/gateway call with automatic retry on rate limits.
 *
 * Handles both GatewayRateLimitError (opcode 8 etc.) and REST DiscordAPIError
 * with status 429. Waits for the retry_after period before retrying.
 *
 * @param {Function} fn - async function to call
 * @param {object} [options]
 * @param {number} [options.maxRetries=3] - max retry attempts
 * @param {object} [options.log] - logger instance (must have .warn method)
 * @param {string} [options.context=''] - description for log messages
 * @returns {Promise<*>} result of fn()
 */
async function withRateLimitRetry(fn, options = {}) {
	const { maxRetries = 3, log = null, context = '' } = options

	for (let attempt = 0; attempt <= maxRetries; attempt++) {
		try {
			return await fn()
		} catch (err) {
			const isGatewayRateLimit = err.constructor.name === 'GatewayRateLimitError'
			const isRestRateLimit = err.status === 429

			if ((isGatewayRateLimit || isRestRateLimit) && attempt < maxRetries) {
				const retryAfter = (err.data?.retry_after || err.retry_after || 15) * 1000
				if (log) {
					log.warn(`${context ? `${context}: ` : ''}Discord rate limited, retrying in ${retryAfter / 1000}s (attempt ${attempt + 1}/${maxRetries})`)
				}
				await new Promise((resolve) => { setTimeout(resolve, retryAfter + 1000) })
			} else {
				throw err
			}
		}
	}
}

module.exports = { withRateLimitRetry }
