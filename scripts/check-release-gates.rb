#!/usr/bin/env ruby
# Structural regression test for the release DAG. GitHub's default job
# condition requires every prerequisite to succeed; do not replace it with
# always() or a condition that accepts skipped jobs.
require 'yaml'
release = YAML.load_file(File.join(__dir__, '../.github/workflows/release.yml'))['jobs']
verify = YAML.load_file(File.join(__dir__, '../.github/workflows/verify.yml'))['jobs']
required = %w[verify binary edge runtime]
raise 'publish must require all artifacts and verification' unless (required - release.fetch('publish').fetch('needs')).empty?
%w[binary edge runtime].each do |name|
  raise "#{name} can publish before verification" unless Array(release.fetch(name)['needs']).include?('verify')
end
%w[go install-script web integration].each do |name|
  raise "missing required #{name} verification" unless verify.key?(name)
  raise "#{name} can be conditionally skipped" if verify[name].key?('if') || verify[name]['continue-on-error']
  verify[name].fetch('steps').each do |step|
    raise "#{name} ignores a failed check" if step['continue-on-error']
  end
end
required.concat(['publish']).each do |name|
  job = release.fetch(name)
  raise "#{name} overrides success prerequisites" if job.key?('if') || job['continue-on-error']
end
# Negative cases: any failed, cancelled, or skipped required check must make
# publication ineligible; the passing case must remain eligible.
eligible = ->(states) { release.fetch('publish').fetch('needs').all? { |id| states[id] == 'success' } }
passing = required.to_h { |id| [id, 'success'] }
raise 'passing release is blocked' unless eligible.call(passing)
required.reject { |id| id == 'publish' }.each do |id|
  %w[failure cancelled skipped].each do |state|
    raise "publication accepts #{id}=#{state}" if eligible.call(passing.merge(id => state))
  end
end
puts 'Release gates: passing case and all failure/cancellation/skip cases verified.'
