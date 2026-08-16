class Keygrp < Formula
  desc "Run a CLI with keychain-backed environment variables"
  homepage "https://github.com/mrchi/keygrp"
  version "0.1.1"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/mrchi/keygrp/releases/download/v#{version}/keygrp-darwin-arm64.tar.gz"
      sha256 "96d22fc5257c93da2512c1a15a8fe67dd886afcc8548debc04258c72c48f4a6c"
    end
    on_intel do
      url "https://github.com/mrchi/keygrp/releases/download/v#{version}/keygrp-darwin-amd64.tar.gz"
      sha256 "1a6f0178cb1ca93daacccba5f8b74905c57cf9c7c135417b7b5939c82b1c0bdb"
    end
  end
  on_linux do
    on_intel do
      url "https://github.com/mrchi/keygrp/releases/download/v#{version}/keygrp-linux-amd64.tar.gz"
      sha256 "eb6052dd15c0276917035df14a8855af664a878f50bfeae347244830604b119b"
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
