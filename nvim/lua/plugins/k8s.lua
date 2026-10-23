-- Kubernetes: kubectl.nvim (browse the cluster in a buffer) + yaml-companion
-- (yamlls schema support for manifests, incl. CRDs fetched from the cluster).
--   :Kubectl get pods -A   :Kubectx   :Kubens   (gs = sort column)
-- yamlls is installed via mason (lsp.lua) — this file only configures the
-- schema layer on top of it.
return {
  {
    "Ramilito/kubectl.nvim",
    version = "2.*",
    cmd = { "Kubectl", "Kubectx", "Kubens" },
    config = function()
      require("kubectl").setup({})
    end,
  },
  {
    "mosheavni/yaml-companion.nvim",
    dependencies = { "neovim/nvim-lspconfig" },
    config = function()
      -- Native vim.lsp wiring (README: setup() returns the yamlls config).
      -- The yamlls binary comes from mason (ensure_installed in lsp.lua);
      -- schemas: Schema Store by default, + cluster CRDs via
      -- :YamlFetchClusterCRD / :YamlBrowseClusterCRDs.
      local cfg = require("yaml-companion").setup({})
      vim.lsp.config("yamlls", cfg)
      vim.lsp.enable("yamlls")

      -- On-demand kubeconform validation (nvim-lint ships no kubeconform
      -- linter; yamlls schemas cover in-editor validation, kubeconform is
      -- the strict pre-merge/CI check). No-op when the binary is absent.
      vim.api.nvim_create_user_command("Kubeconform", function()
        local file = vim.fn.expand("%:p")
        -- vim.fn.executable() returns 1 or 0 (both truthy in Lua), so the
        -- inverted check must use ~= 1 to detect a missing binary.
        -- Audit finding P2 (truthiness bug).
        if vim.fn.executable("kubeconform") ~= 1 then
          print("kubeconform not installed (brew install kubeconform)")
          return
        end
        -- One invocation; -summary gives a one-line summary (status text),
        -- and the regular output is the per-file diagnostic list. We print
        -- both — the original code called kubeconform twice (once with
        -- -summary, once without) which doubled validation time on large
        -- manifests.
        local out = vim.fn.systemlist({ "kubeconform", "-strict", "-summary", file })
        if vim.v.shell_error == 0 then
          print("kubeconform: valid manifest(s)")
        else
          print("kubeconform: validation errors (see output)")
        end
        print(table.concat(out, "\n"))
      end, { desc = "k8s: kubeconform current file" })
    end,
  },
}