/* eslint-disable max-classes-per-file */
const NodeGeocoder = require('node-geocoder')
const { Cacheable } = require('cacheable')
const KeyvSqlite = require('@keyv/sqlite').default
const { performance } = require('perf_hooks')
const emojiFlags = require('country-code-emoji')
const path = require('path')
const fs = require('fs')
const NominatimGeocoder = require('./nominatimGeocoder')
const metrics = require('./metrics')

class NominatimGeocoderConverter extends NominatimGeocoder {
	async reverse(obj) {
		const result = await super.reverse(obj.lat, obj.lon)

		if (!result) return null
		if (result.error) return result

		let countryCode = result.address.country_code
		if (countryCode) {
			countryCode = countryCode.toUpperCase()
		}

		let latitude = result.lat
		if (latitude) {
			latitude = parseFloat(latitude)
		}

		let longitude = result.lon
		if (longitude) {
			longitude = parseFloat(longitude)
		}

		return [{
			latitude,
			longitude,
			formattedAddress: result.display_name,
			country: result.address.country,
			city: result.address.city || result.address.town || result.address.village || result.address.hamlet,
			state: result.address.state,
			zipcode: result.address.postcode,
			streetName: result.address.road || result.address.quarter || result.address.cycleway,
			streetNumber: result.address.house_number,
			countryCode,
			neighbourhood: result.address.neighbourhood || '',
			suburb: result.address.suburb || '',
			town: result.address.town || '',
			village: result.address.village || '',
			shop: result.address.shop || '',
		}]
	}
}

class CachingGeocoder {
	constructor(config, log, mustache, cacheFilename) {
		this.log = log || console
		this.config = config
		this.mustache = mustache
		if (cacheFilename) {
			const cacheDir = path.join(__dirname, '../../.cache')
			fs.mkdirSync(cacheDir, { recursive: true })
			const dbPath = path.join(cacheDir, `${cacheFilename}.sqlite`)
			this.cache = new Cacheable({
				primary: { ttl: '24h' },
				secondary: new KeyvSqlite(`sqlite://${dbPath}`),
			})
		} else {
			this.cache = null
		}
		this.timeout = this.config.tuning.geocodingTimeout || 5000
		this.stats = {
			calls: 0, totalMs: 0, inFlight: 0, cacheHits: 0, errors: 0,
		}
		this.consecutiveErrors = 0
		this.failureThreshold = config.tuning.geocodingFailureThreshold || 5
		this.cooldownMs = config.tuning.geocodingCooldownMs || 30000
		this.circuitOpenSince = 0
	}

	getGeocoder() {
		switch (this.config.geocoding.provider.toLowerCase()) {
			case 'nominatim': {
				return new NominatimGeocoderConverter(this.config.geocoding.providerURL, this.timeout)
			}
			case 'google': {
				return NodeGeocoder({
					provider: 'google',
					httpAdapter: 'https',
					apiKey: this.config.geocoding.geocodingKey[Math.floor(Math.random() * this.config.geocoding.geocodingKey.length)],
					timeout: this.timeout,
				})
			}
			default:
			{
				return NodeGeocoder({
					provider: 'openstreetmap',
					formatterPattern: this.config.locale.addressFormat,
					timeout: this.timeout,
				})
			}
		}
	}

	async getAddress(locationObject) {
		if (this.config.geocoding.provider.toLowerCase() === 'none') {
			return { addr: 'Unknown', flag: '' }
		}

		if (this.config.geocoding.forwardOnly) {
			return { addr: 'Unknown', flag: '' }
		}

		const doGeolocate = async () => {
			// Circuit breaker: skip calls when geocoder is repeatedly failing
			const now = Date.now()
			if (this.consecutiveErrors >= this.failureThreshold) {
				if (now - this.circuitOpenSince < this.cooldownMs) {
					this.stats.circuitBreaks = (this.stats.circuitBreaks || 0) + 1
					metrics.geocodeTotal.inc({ result: 'circuit_break' })
					return { addr: 'Unknown', flag: '' }
				}
				// Half-open: allow one probe request
			}

			this.stats.inFlight++
			metrics.geocodeInFlight.inc()
			try {
				const startTime = performance.now()
				const geocoder = this.getGeocoder()
				const r = await geocoder.reverse(locationObject)
				if (!r || r.error) {
					this.log.error(`getAddress: failed to fetch data - ${!r ? 'no result' : `${r.error}`}`)
					this.stats.errors++
					this.consecutiveErrors++
					metrics.geocodeTotal.inc({ result: 'error' })
					if (this.consecutiveErrors >= this.failureThreshold) this.circuitOpenSince = Date.now()
					return { addr: 'Unknown', flag: '' }
				}

				const result = r[0]
				const endTime = performance.now()
				const elapsed = endTime - startTime
				this.stats.calls++
				this.stats.totalMs += elapsed
				metrics.geocodeDuration.observe(elapsed / 1000)
				metrics.geocodeTotal.inc({ result: 'success' })
				this.consecutiveErrors = 0;
				(this.config.logger.timingStats ? this.log.verbose : this.log.debug)(`Geocode ${locationObject.lat},${locationObject.lon} (${elapsed} ms)`)

				const flag = emojiFlags.countryCodeEmoji(result.countryCode)
				if (!this.addressDts) {
					this.addressDts = this.mustache.compile(this.config.locale.addressFormat)
				}
				result.addr = this.addressDts(result)
				result.flag = flag || ''

				return this.escapeAddress(result)
			} catch (err) {
				this.stats.errors++
				this.consecutiveErrors++
				metrics.geocodeTotal.inc({ result: 'error' })
				if (this.consecutiveErrors >= this.failureThreshold) this.circuitOpenSince = Date.now()
				this.log.error('getAddress: failed to fetch data', err)
				return { addr: 'Unknown', flag: '' }
			} finally {
				this.stats.inFlight--
				metrics.geocodeInFlight.dec()
			}
		}

		if (this.config.geocoding.cacheDetail === 0) {
			return doGeolocate()
		}

		const cacheKey = `${String(+locationObject.lat.toFixed(this.config.geocoding.cacheDetail))}-${String(+locationObject.lon.toFixed(this.config.geocoding.cacheDetail))}`
		if (this.cache) {
			const cachedResult = await this.cache.get(cacheKey)
			if (cachedResult) {
				this.stats.cacheHits++
				metrics.geocodeTotal.inc({ result: 'cache_hit' })
				return this.escapeAddress(cachedResult)
			}
		}

		const result = await doGeolocate()
		if (this.cache) {
			await this.cache.set(cacheKey, result)
		}

		return result
	}

	getStats() {
		return {
			...this.stats,
			avgMs: this.stats.calls > 0 ? Math.round(this.stats.totalMs / this.stats.calls) : 0,
		}
	}

	resetStats() {
		this.stats = {
			calls: 0, totalMs: 0, inFlight: this.stats.inFlight, cacheHits: 0, errors: 0,
		}
	}

	// eslint-disable-next-line class-methods-use-this
	escapeJsonString(s) {
		if (!s) return s
		return s.replace(/"/g, '\'\'').replace(/\n/g, ' ').replace(/\\/g, '?')
	}

	escapeAddress(a) {
		a.streetName = this.escapeJsonString(a.streetName)
		a.addr = this.escapeJsonString(a.addr)
		return a
	}
}

module.exports = CachingGeocoder
