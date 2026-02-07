class EmergentCli < Formula
  desc "Command-line interface for the Emergent Knowledge Base platform"
  homepage "https://github.com/Emergent-Comapny/emergent"
  version "0.2.0"
  license "Apache-2.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/Emergent-Comapny/emergent/releases/download/cli-v0.2.0/emergent-cli-darwin-arm64.tar.gz"
      sha256 "7b859f987977ee48f97910305d8dd2c26fb44784da338e1be5f9c89b727584e2"
    else
      url "https://github.com/Emergent-Comapny/emergent/releases/download/cli-v0.2.0/emergent-cli-darwin-amd64.tar.gz"
      sha256 "60dbf7f031da86c02bde6578164c6b0e4c7f110921d314de7f1e05e608495741"
    end
  end

  on_linux do
    if Hardware::CPU.arm? && Hardware::CPU.is_64_bit?
      url "https://github.com/Emergent-Comapny/emergent/releases/download/cli-v0.2.0/emergent-cli-linux-arm64.tar.gz"
      sha256 "314494548680ea34071c4dae9c091b1322b918c95f33323ed924c549351cc890"
    else
      url "https://github.com/Emergent-Comapny/emergent/releases/download/cli-v0.2.0/emergent-cli-linux-amd64.tar.gz"
      sha256 "dbc14b26eb7e61c8c9f7224e68ca5e62977f8f922d3eee468cbd406e8fc09275"
    end
  end

  def install
    bin.install "emergent"
    
    (bash_completion/"emergent").write Utils.safe_popen_read("#{bin}/emergent", "completion", "bash")
    (zsh_completion/"_emergent").write Utils.safe_popen_read("#{bin}/emergent", "completion", "zsh")
    (fish_completion/"emergent.fish").write Utils.safe_popen_read("#{bin}/emergent", "completion", "fish")
  end

  test do
    assert_match "Emergent CLI", shell_output("#{bin}/emergent version")
  end
end
