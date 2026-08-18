class Keygrp < Formula
  desc "Run a CLI with keychain-backed environment variables"
  homepage "https://github.com/mrchi/keygrp"
  version "0.1.2"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/mrchi/keygrp/releases/download/v#{version}/keygrp-darwin-arm64.tar.gz"
      sha256 "1826a78877b11f51d83e437f17f4ec334f11a37eb17c67b8477b31bd7cd556fc"
    end
    on_intel do
      url "https://github.com/mrchi/keygrp/releases/download/v#{version}/keygrp-darwin-amd64.tar.gz"
      sha256 "4bd836b3fedd1a3ad2131119dec468d2256cc40e63094e4d62b20c4aa1555a9c"
    end
  end
  on_linux do
    on_intel do
      url "https://github.com/mrchi/keygrp/releases/download/v#{version}/keygrp-linux-amd64.tar.gz"
      sha256 "81b75a6a602786ed01f62b757557ad8fd5f14f0f056d02580cf3b459827652a3"
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
