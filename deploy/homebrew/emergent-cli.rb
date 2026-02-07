class EmergentCli < Formula
  desc "Command-line interface for the Emergent Knowledge Base platform"
  homepage "https://github.com/Emergent-Comapny/emergent"
  version "0.2.1"
  license "Apache-2.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/Emergent-Comapny/emergent/releases/download/cli-v0.2.1/emergent-cli-darwin-arm64.tar.gz"
      sha256 "f4da3e7e44476a7e96eee0dedf15b8ffaa72719a434158324d742878349d6db8"
    else
      url "https://github.com/Emergent-Comapny/emergent/releases/download/cli-v0.2.1/emergent-cli-darwin-amd64.tar.gz"
      sha256 "c527a56ca3d85b78302a018a9a1570dcabfb0cc511842ab85f462b7a08a682e7"
    end
  end

  on_linux do
    if Hardware::CPU.arm? && Hardware::CPU.is_64_bit?
      url "https://github.com/Emergent-Comapny/emergent/releases/download/cli-v0.2.1/emergent-cli-linux-arm64.tar.gz"
      sha256 "1a6a3e175f3e4231d352e53fd5ec2b67047d21603a9aadce7aa88a7ee4cdcdcc"
    else
      url "https://github.com/Emergent-Comapny/emergent/releases/download/cli-v0.2.1/emergent-cli-linux-amd64.tar.gz"
      sha256 "5721f9cfcac35c969d769d593853356ec834f0e575a44191887b3b9c508c2968"
    end
  end

  def install
    if OS.mac? && Hardware::CPU.arm?
      bin.install "emergent-cli-darwin-arm64" => "emergent"
    elsif OS.mac? && Hardware::CPU.intel?
      bin.install "emergent-cli-darwin-amd64" => "emergent"
    elsif OS.linux? && Hardware::CPU.arm? && Hardware::CPU.is_64_bit?
      bin.install "emergent-cli-linux-arm64" => "emergent"
    elsif OS.linux? && Hardware::CPU.intel?
      bin.install "emergent-cli-linux-amd64" => "emergent"
    else
      bin.install "emergent"
    end
    
    (bash_completion/"emergent").write Utils.safe_popen_read("#{bin}/emergent", "completion", "bash")
    (zsh_completion/"_emergent").write Utils.safe_popen_read("#{bin}/emergent", "completion", "zsh")
    (fish_completion/"emergent.fish").write Utils.safe_popen_read("#{bin}/emergent", "completion", "fish")
  end

  test do
    assert_match "Emergent CLI", shell_output("#{bin}/emergent version")
  end
end
