const RdmScanner = require('./rdmScanner')
const GolbatScanner = require('./golbatScanner')

function createScanner(db, scannerType) {
	if (!db) return null

	switch (scannerType) {
		case 'rdm':
			return new RdmScanner(db)
		default:
			return new GolbatScanner(db)
	}
}

module.exports = { createScanner }
