-- Entry point: options & keymaps first, then lazy.nvim
require("config.options")
require("config.keymaps")
-- context tracking (last real source file) must be live before any UI loads
require("config.context").setup()
require("config.lazy")
