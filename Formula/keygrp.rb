class Keygrp < Formula
  desc "Run a CLI with keychain-backed environment variables"
  homepage "https://github.com/mrchi/keygrp"
  version "0.2.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/mrchi/keygrp/releases/download/v#{version}/keygrp-darwin-arm64.tar.gz"
      sha256 "55f661d19e6ee9cf1d3b6201b15b5c840d37b537e6d68c017c98702f3cc3f3b7"
    end
    on_intel do
      url "https://github.com/mrchi/keygrp/releases/download/v#{version}/keygrp-darwin-amd64.tar.gz"
      sha256 "c7491793623d63203661541d4b96048192a4dec5105c2d62701b432fbac30129"
    end
  end
  on_linux do
    on_intel do
      url "https://github.com/mrchi/keygrp/releases/download/v#{version}/keygrp-linux-amd64.tar.gz"
      sha256 "2df23997f28dd6ea6ed8fb55306d982cb3b3bc9cf86fabc19e8bce2e2b4d0132"
    end
  end

  def install
    bin.install "kg", "kgx"
  end

  def caveats
    <<~EOS
      Run once to install shell completion, create a starter config (without
      touching an existing one), and authorize keychain access:

        kg init
    EOS
  end

  test do
    assert_match "kg", shell_output("#{bin}/kg --help")
  end
end
