package ldapsrv

// subschemaDN is the DN advertised on the root DSE as subschemaSubentry.
// OpenLDAP serves the same entry at cn=Subschema. Clients such as Apache
// Directory Studio read it with a base-scope search and refuse to enable
// schema-aware features when the entry is missing.
const subschemaDN = "cn=subschema"

// attributeTypes, objectClasses, ldapSyntaxes, and matchingRules are the
// subschema, written in the OpenLDAP / RFC 4512 description format.
// Definitions follow core, cosine, and inetOrgPerson for the classes this
// directory publishes. group is a second name of groupOfNames: role
// entries carry both so Active Directory clients and OpenLDAP clients
// see a group, without a second structural class.
//
// Descriptions are a closed set. Every SUP, MUST, MAY, EQUALITY, and
// SYNTAX name used here is defined here too.

var ldapSyntaxes = []string{
	"( 1.3.6.1.4.1.1466.115.121.1.3 DESC 'Attribute Type Description' )",
	"( 1.3.6.1.4.1.1466.115.121.1.12 DESC 'DN' )",
	"( 1.3.6.1.4.1.1466.115.121.1.15 DESC 'Directory String' )",
	"( 1.3.6.1.4.1.1466.115.121.1.26 DESC 'IA5 String' )",
	"( 1.3.6.1.4.1.1466.115.121.1.27 DESC 'INTEGER' )",
	"( 1.3.6.1.4.1.1466.115.121.1.30 DESC 'Matching Rule Description' )",
	"( 1.3.6.1.4.1.1466.115.121.1.37 DESC 'Object Class Description' )",
	"( 1.3.6.1.4.1.1466.115.121.1.38 DESC 'OID' )",
	"( 1.3.6.1.4.1.1466.115.121.1.50 DESC 'Telephone Number' )",
	"( 1.3.6.1.4.1.1466.115.121.1.54 DESC 'LDAP Syntax Description' )",
	"( 1.3.6.1.4.1.1466.115.121.1.58 DESC 'Substring Assertion' )",
}

var matchingRules = []string{
	"( 2.5.13.0 NAME 'objectIdentifierMatch' SYNTAX 1.3.6.1.4.1.1466.115.121.1.38 )",
	"( 2.5.13.1 NAME 'distinguishedNameMatch' SYNTAX 1.3.6.1.4.1.1466.115.121.1.12 )",
	"( 2.5.13.2 NAME 'caseIgnoreMatch' SYNTAX 1.3.6.1.4.1.1466.115.121.1.15 )",
	"( 2.5.13.4 NAME 'caseIgnoreSubstringsMatch' SYNTAX 1.3.6.1.4.1.1466.115.121.1.58 )",
	"( 2.5.13.20 NAME 'telephoneNumberMatch' SYNTAX 1.3.6.1.4.1.1466.115.121.1.50 )",
	"( 2.5.13.21 NAME 'telephoneNumberSubstringsMatch' SYNTAX 1.3.6.1.4.1.1466.115.121.1.58 )",
	"( 1.3.6.1.4.1.1466.109.114.1 NAME 'caseIgnoreIA5Match' SYNTAX 1.3.6.1.4.1.1466.115.121.1.26 )",
	"( 1.3.6.1.4.1.1466.109.114.2 NAME 'caseIgnoreIA5SubstringsMatch' SYNTAX 1.3.6.1.4.1.1466.115.121.1.58 )",
}

var attributeTypes = []string{
	"( 2.5.4.0 NAME 'objectClass' EQUALITY objectIdentifierMatch SYNTAX 1.3.6.1.4.1.1466.115.121.1.38 )",
	"( 2.5.4.49 NAME 'distinguishedName' EQUALITY distinguishedNameMatch SYNTAX 1.3.6.1.4.1.1466.115.121.1.12 )",
	"( 2.5.4.41 NAME 'name' EQUALITY caseIgnoreMatch SUBSTR caseIgnoreSubstringsMatch SYNTAX 1.3.6.1.4.1.1466.115.121.1.15 )",
	"( 2.5.4.3 NAME 'cn' SUP name )",
	"( 2.5.4.4 NAME 'sn' SUP name )",
	"( 2.5.4.10 NAME 'o' SUP name )",
	"( 2.5.4.11 NAME 'ou' SUP name )",
	"( 2.5.4.13 NAME 'description' EQUALITY caseIgnoreMatch SUBSTR caseIgnoreSubstringsMatch SYNTAX 1.3.6.1.4.1.1466.115.121.1.15 )",
	"( 2.5.4.31 NAME 'member' SUP distinguishedName )",
	"( 0.9.2342.19200300.100.1.25 NAME ( 'dc' 'domainComponent' ) EQUALITY caseIgnoreIA5Match SUBSTR caseIgnoreIA5SubstringsMatch SYNTAX 1.3.6.1.4.1.1466.115.121.1.26 SINGLE-VALUE )",
	"( 0.9.2342.19200300.100.1.1 NAME ( 'uid' 'userid' ) EQUALITY caseIgnoreMatch SUBSTR caseIgnoreSubstringsMatch SYNTAX 1.3.6.1.4.1.1466.115.121.1.15 )",
	"( 0.9.2342.19200300.100.1.3 NAME ( 'mail' 'rfc822Mailbox' ) EQUALITY caseIgnoreIA5Match SUBSTR caseIgnoreIA5SubstringsMatch SYNTAX 1.3.6.1.4.1.1466.115.121.1.26 )",
	"( 0.9.2342.19200300.100.1.41 NAME 'mobile' EQUALITY telephoneNumberMatch SUBSTR telephoneNumberSubstringsMatch SYNTAX 1.3.6.1.4.1.1466.115.121.1.50 )",
	"( 2.16.840.1.113730.3.1.241 NAME 'displayName' EQUALITY caseIgnoreMatch SUBSTR caseIgnoreSubstringsMatch SYNTAX 1.3.6.1.4.1.1466.115.121.1.15 SINGLE-VALUE )",
	"( 1.2.840.113556.1.2.102 NAME 'memberOf' EQUALITY distinguishedNameMatch SYNTAX 1.3.6.1.4.1.1466.115.121.1.12 NO-USER-MODIFICATION USAGE directoryOperation )",
	"( 1.3.6.1.4.1.1466.101.120.5 NAME 'namingContexts' SYNTAX 1.3.6.1.4.1.1466.115.121.1.12 USAGE dSAOperation )",
	"( 1.3.6.1.4.1.1466.101.120.15 NAME 'supportedLDAPVersion' SYNTAX 1.3.6.1.4.1.1466.115.121.1.27 USAGE dSAOperation )",
	"( 1.3.6.1.4.1.1466.101.120.1 NAME 'vendorName' EQUALITY caseIgnoreMatch SYNTAX 1.3.6.1.4.1.1466.115.121.1.15 SINGLE-VALUE USAGE dSAOperation )",
	"( 2.5.18.10 NAME 'subschemaSubentry' EQUALITY distinguishedNameMatch SYNTAX 1.3.6.1.4.1.1466.115.121.1.12 NO-USER-MODIFICATION USAGE directoryOperation )",
	"( 2.5.21.5 NAME 'attributeTypes' SYNTAX 1.3.6.1.4.1.1466.115.121.1.3 USAGE directoryOperation )",
	"( 2.5.21.6 NAME 'objectClasses' SYNTAX 1.3.6.1.4.1.1466.115.121.1.37 USAGE directoryOperation )",
	"( 2.5.21.4 NAME 'matchingRules' SYNTAX 1.3.6.1.4.1.1466.115.121.1.30 USAGE directoryOperation )",
	"( 1.3.6.1.4.1.1466.101.120.16 NAME 'ldapSyntaxes' SYNTAX 1.3.6.1.4.1.1466.115.121.1.54 USAGE directoryOperation )",
}

var objectClasses = []string{
	"( 2.5.6.0 NAME 'top' ABSTRACT MUST objectClass )",
	"( 2.5.6.6 NAME 'person' DESC 'RFC4519: a person' SUP top STRUCTURAL MUST ( sn $ cn ) MAY description )",
	"( 2.5.6.7 NAME 'organizationalPerson' DESC 'RFC4519: an organizational person' SUP person STRUCTURAL MAY ou )",
	"( 2.16.840.1.113730.3.2.2 NAME 'inetOrgPerson' DESC 'RFC2798: Internet Organizational Person' SUP organizationalPerson STRUCTURAL MAY ( displayName $ mail $ mobile $ uid ) )",
	"( 2.5.6.9 NAME ( 'groupOfNames' 'group' ) DESC 'RFC4519: a group of names (DNs)' SUP top STRUCTURAL MUST ( member $ cn ) MAY ( description $ ou $ o ) )",
	"( 2.5.6.4 NAME 'organization' DESC 'RFC4519: an organization' SUP top STRUCTURAL MUST o MAY description )",
	"( 2.5.6.5 NAME 'organizationalUnit' DESC 'RFC4519: an organizational unit' SUP top STRUCTURAL MUST ou MAY description )",
	"( 1.3.6.1.4.1.1466.344 NAME 'dcObject' DESC 'RFC4519: domain component object' SUP top AUXILIARY MUST dc )",
	"( 0.9.2342.19200300.100.4.13 NAME 'domain' DESC 'RFC4524: a domain' SUP top STRUCTURAL MUST dc MAY ( o $ description ) )",
	"( 2.5.20.1 NAME 'subschema' AUXILIARY MAY ( objectClasses $ attributeTypes $ matchingRules $ ldapSyntaxes ) )",
	"( 1.3.6.1.4.1.4203.1.4.1 NAME 'OpenLDAProotDSE' DESC 'OpenLDAP Root DSE object' SUP top STRUCTURAL MAY ( namingContexts $ supportedLDAPVersion $ vendorName $ subschemaSubentry ) )",
}

func subschemaEntry() *Entry {
	return &Entry{
		DN: subschemaDN,
		Attrs: map[string][]string{
			"objectclass":    {"top", "subschema"},
			"cn":             {"subschema"},
			"attributetypes": attributeTypes,
			"objectclasses":  objectClasses,
			"ldapsyntaxes":   ldapSyntaxes,
			"matchingrules":  matchingRules,
		},
		operational: map[string]bool{
			"attributetypes": true,
			"objectclasses":  true,
			"ldapsyntaxes":   true,
			"matchingrules":  true,
		},
	}
}
