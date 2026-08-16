class Keygrp < Formula
  desc "Run a CLI with keychain-backed environment variables"
  homepage "https://github.com/mrchi/keygrp"
  version "0.1.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/mrchi/keygrp/releases/download/v#{version}/keygrp-darwin-arm64.tar.gz"
      sha256 "cadf6e45fae439bba5d51f68aaf97d01b5ce6022eacfbc3ce88b901b5abff787"
    end
    on_intel do
      url "https://github.com/mrchi/keygrp/releases/download/v#{version}/keygrp-darwin-amd64.tar.gz"
      sha256 "96a9deeff29049f8150550ac6552919c129f0ec57e6700926ab7e572b3f5e670"
    end
  end
  on_linux do
    on_intel do
      url "https://github.com/mrchi/keygrp/releases/download/v#{version}/keygrp-linux-amd64.tar.gz"
      sha256 "510eec2584c60942b4c49b35ea97f72b3f5ef63a528376942344c578f3a78180"
    end
  end

  def install
    bin.install "kg", "kgx"
  end

  def caveats
    <<~EOS
      Run once to install shell completion, create a starter config, and
      authorize keychain access:

        kg init
    EOS
  end

  test do
    assert_match "kg", shell_output("#{bin}/kg --help")
  end
end
